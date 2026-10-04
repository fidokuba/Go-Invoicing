import { test, expect, type APIRequestContext } from "@playwright/test";

/**
 * Cancelling invoices and deleting customers, against the same real Go
 * API + PostgreSQL stack as workflow.spec.ts (see e2e/README.md):
 *
 * customer with a draft invoice → deleting the customer is refused while
 * that invoice is open → the invoice is cancelled (kept, not deleted) →
 * the customer can now be deleted → the cancelled invoice still shows the
 * customer's name, owes nothing, and still renders a PDF. Also proves a
 * plain "user" role is refused (403) by both new endpoints.
 */

const runId = Date.now();
const adminEmail = `e2e-cancel-${runId}@example.com`;
const userEmail = `e2e-cancel-user-${runId}@example.com`;
const password = "correct-horse-battery-staple";
const customerName = `Cancel Customer ${runId}`;
const productName = `Cancel Product ${runId}`;

async function login(request: APIRequestContext, baseURL: string, email: string): Promise<string> {
  const response = await request.post(`${baseURL}/api/v1/auth/login`, { data: { email, password } });
  expect(response.ok()).toBe(true);
  return ((await response.json()) as { token: string }).token;
}

test("cancel an invoice, then delete its customer", async ({ page, request, baseURL }) => {
  // --- Register + sign in --------------------------------------------------
  await page.goto("/register");
  await page.getByLabel("Organisation name").fill(`Cancel Co ${runId}`);
  await page.getByLabel("Your name").fill("Cancel Admin");
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("checkbox", { name: "I agree to the Terms & Conditions" }).check();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/login\?registered=1/);

  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard/);

  // --- Customer, product, draft invoice -------------------------------------
  await page.goto("/customers/new");
  await page.getByLabel("Name", { exact: true }).fill(customerName);
  await page.getByRole("button", { name: "Create customer" }).click();
  await expect(page).toHaveURL(/\/customers\/[0-9a-f-]+$/);
  const customerUrl = page.url();
  const customerId = customerUrl.split("/").pop()!;

  await page.goto("/products/new");
  await page.getByLabel("Name").fill(productName);
  await page.getByLabel("SKU").fill(`SKU-C-${runId}`);
  await page.getByLabel(/Price/).fill("100.00");
  await page.getByRole("button", { name: "Create product" }).click();
  await expect(page).toHaveURL(/\/products\/[0-9a-f-]+$/);

  await page.goto("/invoices/new");
  await page.getByLabel("Customer").selectOption({ label: customerName });
  await page.getByLabel("Product (optional)").selectOption({ label: productName });
  await page.getByLabel("Quantity").fill("1");
  await page.getByRole("button", { name: "Create draft invoice" }).click();
  await expect(page).toHaveURL(/\/invoices\/[0-9a-f-]+$/);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^INV-/);
  const invoiceUrl = page.url();
  const invoiceId = invoiceUrl.split("/").pop()!;
  // The customer name now comes back on the invoice itself.
  await expect(page.getByText(customerName)).toBeVisible();

  // --- Deleting the customer is refused while the draft is open --------------
  await page.goto(customerUrl);
  await page.getByRole("button", { name: "Delete customer" }).click();
  const deleteDialog = page.getByRole("dialog");
  await deleteDialog.getByRole("button", { name: "Delete customer" }).click();
  await expect(deleteDialog.getByText(/customer has open invoices/i)).toBeVisible();
  await deleteDialog.getByRole("button", { name: "Keep customer" }).click();

  // --- A plain "user" can do neither (403), even via the API directly ------
  const adminToken = await login(request, baseURL!, adminEmail);
  const createUser = await request.post(`${baseURL}/api/v1/users`, {
    headers: { Authorization: `Bearer ${adminToken}` },
    data: { name: "Plain User", email: userEmail, password, role: "user" },
  });
  expect(createUser.status()).toBe(201);
  const userToken = await login(request, baseURL!, userEmail);
  const userCancel = await request.post(`${baseURL}/api/v1/invoices/${invoiceId}/cancel`, {
    headers: { Authorization: `Bearer ${userToken}` },
  });
  expect(userCancel.status()).toBe(403);
  const userDelete = await request.delete(`${baseURL}/api/v1/customers/${customerId}`, {
    headers: { Authorization: `Bearer ${userToken}` },
  });
  expect(userDelete.status()).toBe(403);

  // --- Cancel the invoice ----------------------------------------------------
  await page.goto(invoiceUrl);
  await expect(page.getByRole("heading", { level: 1 })).toHaveText(/^INV-/);
  await page.getByRole("button", { name: "Cancel invoice" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Cancel invoice" }).click();

  await expect(page.getByText("This invoice has been cancelled.")).toBeVisible();
  await expect(page.getByText("Cancelled", { exact: true }).first()).toBeVisible();
  await expect(page.getByRole("button", { name: "Cancel invoice" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Send invoice" })).toHaveCount(0);
  await expect(page.getByRole("button", { name: "Record payment" })).toHaveCount(0);

  // --- Now the customer can be deleted --------------------------------------
  await page.goto(customerUrl);
  await page.getByRole("button", { name: "Delete customer" }).click();
  await page.getByRole("dialog").getByRole("button", { name: "Delete customer" }).click();
  await expect(page).toHaveURL(/\/customers$/);
  await expect(page.getByText(customerName)).toHaveCount(0);

  // --- The cancelled invoice survives, with its customer's name --------------
  await page.goto("/invoices");
  await page.getByLabel("Filter by status").selectOption("cancelled");
  const row = page.getByRole("row").filter({ hasText: customerName });
  await expect(row).toHaveCount(1);
  await expect(row.getByText("Cancelled")).toBeVisible();

  await page.goto(invoiceUrl);
  await expect(page.getByText(customerName)).toBeVisible();

  const invoice = await request.get(`${baseURL}/api/v1/invoices/${invoiceId}`, {
    headers: { Authorization: `Bearer ${adminToken}` },
  });
  const body = (await invoice.json()) as { status: string; amountOutstanding: number; customerName: string };
  expect(body.status).toBe("cancelled");
  expect(body.amountOutstanding).toBe(0);
  expect(body.customerName).toBe(customerName);

  const pdf = await request.get(`${baseURL}/api/v1/invoices/${invoiceId}/pdf`, {
    headers: { Authorization: `Bearer ${adminToken}` },
  });
  expect(pdf.ok()).toBe(true);
  expect((await pdf.body()).subarray(0, 5).toString("latin1")).toBe("%PDF-");
});
