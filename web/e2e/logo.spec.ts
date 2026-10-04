import { test, expect, type Page } from "@playwright/test";

/**
 * Organisation logo, against the real Go API + PostgreSQL + renderer
 * service (see e2e/README.md — this spec also needs the renderer running,
 * since a custom layout's PDF is drawn by it):
 *
 * upload a logo in Settings → Organisation → it appears in the layout
 * builder's Logo block → it is embedded in a custom-layout invoice PDF →
 * removing it brings the builder's placeholder back.
 */

const runId = Date.now();
const adminEmail = `e2e-logo-${runId}@example.com`;
const password = "correct-horse-battery-staple";

/** A small, visibly coloured PNG drawn in the browser itself, so the spec
 * needs no binary fixture file. */
async function makeLogoPng(page: Page): Promise<Buffer> {
  const dataUrl = await page.evaluate(() => {
    const canvas = document.createElement("canvas");
    canvas.width = 240;
    canvas.height = 80;
    const ctx = canvas.getContext("2d")!;
    ctx.fillStyle = "#1d4ed8";
    ctx.fillRect(0, 0, 240, 80);
    ctx.fillStyle = "#ffffff";
    ctx.font = "bold 36px sans-serif";
    ctx.fillText("ACME", 70, 54);
    return canvas.toDataURL("image/png");
  });
  return Buffer.from(dataUrl.split(",")[1], "base64");
}

test("upload a logo and see it in the layout builder and PDF", async ({ page, request, baseURL }) => {
  // --- Register + sign in --------------------------------------------------
  await page.goto("/register");
  await page.getByLabel("Organisation name").fill(`Logo Co ${runId}`);
  await page.getByLabel("Your name").fill("Logo Admin");
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("checkbox", { name: "I agree to the Terms & Conditions" }).check();
  await page.getByRole("button", { name: "Create account" }).click();
  await expect(page).toHaveURL(/\/login\?registered=1/);
  await page.getByLabel("Email").fill(adminEmail);
  await page.getByLabel("Password", { exact: true }).fill(password);
  await page.getByRole("button", { name: "Sign in" }).click();
  await expect(page).toHaveURL(/\/dashboard/);

  const token = await page.evaluate(() => {
    const raw = localStorage.getItem("go-invoicing.session");
    return raw ? (JSON.parse(raw) as { token: string }).token : null;
  });
  const auth = { Authorization: `Bearer ${token}` };

  // --- Upload the logo in Settings → Organisation -----------------------------
  await page.goto("/settings/organisation");
  await expect(page.getByText("No logo")).toBeVisible();
  await page.getByLabel("Logo file").setInputFiles({ name: "logo.png", mimeType: "image/png", buffer: await makeLogoPng(page) });
  await expect(page.getByRole("img", { name: "Organisation logo" })).toBeVisible();
  await expect(page.getByRole("button", { name: "Replace logo" })).toBeVisible();

  // --- A custom layout with a Logo block, made the default ----------------------
  const created = await request.post(`${baseURL}/api/v1/templates`, {
    headers: auth,
    data: {
      name: "Logo Layout",
      definition: {
        root: { props: {} },
        zones: {},
        content: [
          { type: "Logo", props: { id: "Logo-1", text: "[Your Logo]", width: "50%", height: 80, fontSize: 13, textAlign: "left", fontFamily: "system-ui, sans-serif", fontWeight: "400" } },
          { type: "InvoiceTitle", props: { id: "Title-1", text: "INVOICE", width: "100%", height: 0, fontSize: 22, textAlign: "right", fontFamily: "system-ui, sans-serif", fontWeight: "700" } },
        ],
      },
    },
  });
  expect(created.status()).toBe(201);
  const layoutId = ((await created.json()) as { id: string }).id;
  expect((await request.post(`${baseURL}/api/v1/templates/${layoutId}/default`, { headers: auth })).status()).toBe(204);

  // --- The builder's Logo block shows the real logo ------------------------------
  await page.goto(`/settings/invoice-templates/${layoutId}`);
  const canvas = page.frameLocator("iframe").first();
  const builderLogo = canvas.locator("img[src^='blob:']");
  await expect(builderLogo).toBeVisible();
  await expect.poll(() => builderLogo.evaluate((img: HTMLImageElement) => img.naturalWidth)).toBeGreaterThan(0);

  // --- A custom-layout invoice PDF embeds it -------------------------------------
  const customer = await request.post(`${baseURL}/api/v1/customers`, { headers: auth, data: { name: `Logo Customer ${runId}` } });
  const customerId = ((await customer.json()) as { id: string }).id;
  const invoice = await request.post(`${baseURL}/api/v1/invoices`, {
    headers: auth,
    data: {
      customerId,
      issueDate: "2026-10-01",
      dueDate: "2026-10-31",
      lines: [{ description: "Design work", quantity: 1, unitPrice: 10000, vatRate: 0 }],
    },
  });
  expect(invoice.status()).toBe(201);
  const invoiceId = ((await invoice.json()) as { id: string }).id;

  const pdf = await request.get(`${baseURL}/api/v1/invoices/${invoiceId}/pdf`, { headers: auth });
  expect(pdf.ok()).toBe(true);
  const pdfText = (await pdf.body()).toString("latin1");
  expect(pdfText.startsWith("%PDF-")).toBe(true);
  expect(pdfText).toContain("/Subtype /Image");

  // --- Removing the logo brings the builder placeholder back ---------------------
  await page.goto("/settings/organisation");
  await page.getByRole("button", { name: "Remove" }).click();
  await expect(page.getByText("No logo")).toBeVisible();

  await page.goto(`/settings/invoice-templates/${layoutId}`);
  await expect(canvas.getByText("[Your Logo]")).toBeVisible();
  await expect(canvas.locator("img[src^='blob:']")).toHaveCount(0);
});
