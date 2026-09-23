import { test, expect } from "@playwright/test";

/**
 * The one high-value end-to-end workflow (Milestone 12 section 39):
 * registration → login → organisation details → customer → product →
 * invoice → lines → send → PDF → payment → Paid → logout.
 *
 * Runs against a real Go API + real PostgreSQL (see e2e/README.md) —
 * nothing about the backend is mocked. Each run uses a freshly
 * generated organisation/email so it can be re-run against a
 * persistent, non-ephemeral database without unique-constraint
 * collisions.
 */

const runId = Date.now();
const adminEmail = `e2e-admin-${runId}@example.com`;
const adminPassword = "correct-horse-battery-staple";
const organisationName = `E2E Test Co ${runId}`;
const customerName = `E2E Customer ${runId}`;
const productName = `E2E Product ${runId}`;

test.describe.configure({ mode: "serial" });

test("full invoicing workflow: register through paid invoice", async ({ page, request, baseURL }) => {
  // --- Registration ---------------------------------------------------
  await page.goto("/register");
  await page.getByLabel("Organisation name").fill(organisationName);
  await page.getByLabel("Your name").fill("E2E Admin");
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password").fill(adminPassword);
  await page.getByRole("button", { name: "Create account" }).click();

  await expect(page).toHaveURL(/\/login\?registered=1/);
  await expect(page.getByText("Account created")).toBeVisible();

  // --- Login ------------------------------------------------------------
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password").fill(adminPassword);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard/);

  // --- Session restoration after refresh ---------------------------------
  await page.reload();
  await expect(page).toHaveURL(/\/dashboard/);
  await expect(page.getByRole("heading", { name: "Dashboard" })).toBeVisible();

  // --- Organisation details ----------------------------------------------
  await page.goto("/settings/organisation");
  await page.getByLabel("Phone").fill("+44 20 7946 0958");
  await page.getByRole("button", { name: "Save changes" }).click();
  await expect(page.getByText("Organisation details updated.")).toBeVisible();

  // --- Create customer -----------------------------------------------------
  await page.goto("/customers/new");
  await page.getByLabel("Name", { exact: true }).fill(customerName);
  await page.getByLabel("Email").fill(`billing-${runId}@example.com`);
  await page.getByRole("button", { name: "Create customer" }).click();
  await expect(page).toHaveURL(/\/customers\/[0-9a-f-]+$/);
  await expect(page.getByRole("heading", { name: customerName })).toBeVisible();

  // --- Create product --------------------------------------------------
  await page.goto("/products/new");
  await page.getByLabel("Name").fill(productName);
  await page.getByLabel("SKU").fill(`SKU-${runId}`);
  await page.getByLabel(/Price/).fill("150.00");
  await page.getByRole("button", { name: "Create product" }).click();
  await expect(page).toHaveURL(/\/products\/[0-9a-f-]+$/);
  await expect(page.getByRole("heading", { name: productName })).toBeVisible();

  // --- Create invoice, using the product to populate a line -------------
  await page.goto("/invoices/new");
  await page.getByLabel("Customer").selectOption({ label: customerName });
  await page.getByLabel("Product (optional)").selectOption({ label: productName });
  await page.getByLabel("Quantity").fill("2");
  await page.getByLabel(/VAT %/).fill("20");
  await page.getByRole("button", { name: "Create draft invoice" }).click();

  await expect(page).toHaveURL(/\/invoices\/[0-9a-f-]+$/);
  // The invoice detail page is a separate, lazily-loaded route chunk;
  // wait for its own heading (the invoice number) before asserting on
  // any text, so a lingering element from the previous (also
  // lazy-loaded) editor page can never be mistaken for it.
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^INV-/);
  await expect(page.getByText("Draft", { exact: true }).first()).toBeVisible();
  // Preview total (2 × £150.00 + 20% VAT = £360.00) matches what the
  // server actually stored and returned (it legitimately appears more
  // than once: the line total and the invoice total are the same value
  // here, since this invoice has a single line).
  await expect(page.getByText("£360.00").first()).toBeVisible();

  const invoiceUrl = page.url();
  const invoiceId = invoiceUrl.split("/").pop()!;

  // --- Send the invoice ---------------------------------------------------
  await page.getByRole("button", { name: "Send invoice" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Send invoice" }).click();
  // The status badge (there's also an unrelated "Sent" field label once
  // the invoice has a sentAt timestamp, hence .first()).
  await expect(page.getByText("Sent", { exact: true }).first()).toBeVisible();

  // Sending again must be rejected (409) — the UI no longer even offers
  // the button once Sent, which is itself part of what's being proven.
  await expect(page.getByRole("button", { name: "Send invoice" })).toHaveCount(0);

  // --- PDF: verify the authenticated endpoint directly -------------------
  // (Section 39: browser-native PDF viewing in a new tab isn't something
  // Playwright can assert the *content* of meaningfully, so the button
  // is exercised for absence-of-error, and the actual PDF bytes are
  // verified via the same authenticated API call the button makes.)
  const token = await page.evaluate(() => {
    const raw = localStorage.getItem("go-invoicing.session");
    return raw ? (JSON.parse(raw) as { token: string }).token : null;
  });
  expect(token).toBeTruthy();

  const pdfResponse = await request.get(`${baseURL}/api/v1/invoices/${invoiceId}/pdf`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  expect(pdfResponse.ok()).toBe(true);
  expect(pdfResponse.headers()["content-type"]).toContain("application/pdf");
  const pdfBody = await pdfResponse.body();
  expect(pdfBody.subarray(0, 5).toString("latin1")).toBe("%PDF-");

  await page.getByRole("button", { name: "View PDF" }).click();
  // No error surfaces from the PDF fetch/object-URL flow.
  await expect(page.getByRole("alert")).toHaveCount(0);

  // --- Record a payment that exactly settles the invoice ------------------
  await page.getByRole("button", { name: "Record payment" }).click();
  const dialog = page.getByRole("dialog", { name: "Record a payment" });
  await dialog.getByLabel(/Amount/).fill("360.00");
  await dialog.getByLabel("Method").fill("bank_transfer");
  await dialog.getByRole("button", { name: "Record payment" }).click();

  // --- Verify Paid ---------------------------------------------------------
  await expect(page.getByText("This invoice is fully paid.")).toBeVisible();
  await expect(page.getByText("Paid", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Record payment" })).toHaveCount(0);

  // --- Logout ---------------------------------------------------------------
  await page.getByRole("button", { name: "E2E Admin" }).click();
  await page.getByRole("menuitem", { name: "Log out" }).click();
  await expect(page).toHaveURL(/\/login/);

  // A protected page is no longer reachable after logout.
  await page.goto("/dashboard");
  await expect(page).toHaveURL(/\/login/);
});
