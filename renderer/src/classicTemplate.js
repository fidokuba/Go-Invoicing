// classicTemplate.js — Phase 1's proof content: an HTML/CSS reproduction
// of the layout internal/invoice/invoice_pdf_renderer.go currently draws
// with gopdf (header/seller+title, BILL TO, line items table, totals,
// notes). This is deliberately a hand-written HTML template, not yet
// wired to Puck's component set — Phase 1's job is to prove "Go calls
// this service, this service prints real HTML to a real PDF, and Go gets
// valid bytes back," using the existing layout as familiar content to
// verify against. Reusing the actual Puck-authored template definitions
// is Phase 3's concern, once the template data model (Phase 2) exists.

/**
 * escapeHtml prevents every piece of interpolated data — none of it is
 * literal markup, all of it is meant to render as plain text — from
 * being parsed as HTML. This matters here specifically because, unlike
 * the old gopdf renderer (which only ever draws literal text glyphs),
 * this service builds a real HTML document that a real browser engine
 * parses; unescaped data could break the layout or inject markup.
 * @param {unknown} value
 * @returns {string}
 */
function escapeHtml(value) {
  return String(value ?? "")
    .replaceAll("&", "&amp;")
    .replaceAll("<", "&lt;")
    .replaceAll(">", "&gt;")
    .replaceAll('"', "&quot;")
    .replaceAll("'", "&#39;");
}

/** Renders a list of address/contact lines, each escaped, skipping blanks. */
function renderLines(lines) {
  return (lines ?? [])
    .filter((line) => line)
    .map((line) => `<div>${escapeHtml(line)}</div>`)
    .join("");
}

const STATUS_COLORS = {
  DRAFT: "#787878",
  OVERDUE: "#b03030",
  PAID: "#28783c",
};

function renderStatusMarker(statusLabel) {
  if (!statusLabel) return "";
  const color = STATUS_COLORS[statusLabel] ?? "#5a5a5a";
  return `<div class="status-marker" style="background:${color}">${escapeHtml(statusLabel)}</div>`;
}

function renderLineRows(lines, showVAT) {
  return (lines ?? [])
    .map(
      (line) => `
        <tr>
          <td class="desc">${escapeHtml(line.description)}</td>
          <td class="num">${escapeHtml(line.quantity)}</td>
          <td class="num">${escapeHtml(line.unitPrice)}</td>
          ${showVAT ? `<td class="num">${escapeHtml(line.vatRate)}</td>` : ""}
          ${showVAT ? `<td class="num">${escapeHtml(line.vatAmount)}</td>` : ""}
          <td class="num">${escapeHtml(line.total)}</td>
        </tr>`,
    )
    .join("");
}

function renderTotalsRows(data) {
  const rows = data.vatRegistered
    ? [
        ["Subtotal", data.subtotal],
        ["VAT", data.vatTotal],
        ["Total", data.total],
      ]
    : [["Total", data.total]];

  if (data.showPaymentSummary) {
    rows.push(["Amount Paid", data.amountPaid], ["Balance Due", data.amountOutstanding]);
  }

  return rows
    .map(
      ([label, value], i) => `
        <div class="totals-row${label === "Total" ? " totals-row--emphasis" : ""}">
          <span>${escapeHtml(label)}</span>
          <span>${escapeHtml(value)}</span>
        </div>`,
    )
    .join("");
}

/**
 * renderClassicTemplate builds a complete, self-contained HTML document
 * (inline CSS, no external assets) from a RenderRequest-shaped payload —
 * see internal/renderer/client.go's RenderRequest for the Go-side
 * mirror of this exact shape.
 * @param {object} data
 * @returns {string}
 */
export function renderClassicTemplate(data) {
  const showVAT = Boolean(data.vatRegistered);

  return `<!doctype html>
<html>
<head>
<meta charset="utf-8" />
<style>
  @page { size: A4; margin: 40px 40px 50px 40px; }
  body { font-family: "Liberation Serif", Georgia, serif; font-size: 10pt; color: #111; margin: 0; }
  .header { display: flex; justify-content: space-between; align-items: flex-start; margin-bottom: 24px; }
  .seller-name { font-size: 13pt; font-weight: 700; margin-bottom: 6px; }
  .title { font-size: 20pt; font-weight: 700; text-align: right; }
  .metadata { text-align: right; margin-top: 8px; }
  .status-marker {
    display: inline-block; margin-top: 8px; padding: 5px 14px; color: #fff;
    font-size: 11pt; font-weight: 700; border-radius: 2px;
  }
  h2 { font-size: 11pt; margin: 0 0 6px 0; }
  .bill-to { margin-bottom: 18px; }
  table { width: 100%; border-collapse: collapse; margin-bottom: 18px; }
  thead tr { background: #e6e6e6; }
  th, td { padding: 6px; text-align: left; }
  th.num, td.num { text-align: right; }
  tbody tr { border-bottom: 1px solid #d2d2d2; }
  .totals { width: 260px; margin-left: auto; }
  .totals-row { display: flex; justify-content: space-between; padding: 2px 0; }
  .totals-row--emphasis { font-weight: 700; font-size: 11pt; border-top: 1px solid #cbcbcb; margin-top: 4px; padding-top: 4px; }
  .notes h2 { margin-top: 12px; }
</style>
</head>
<body>
  <div class="header">
    <div class="seller">
      <div class="seller-name">${escapeHtml(data.seller?.name)}</div>
      ${renderLines(data.seller?.addressLines)}
      ${renderLines([data.seller?.email, data.seller?.phone, data.seller?.website].filter(Boolean))}
      ${showVAT && data.seller?.taxId ? `<div>VAT Registration Number: ${escapeHtml(data.seller.taxId)}</div>` : ""}
    </div>
    <div class="invoice-meta">
      <div class="title">INVOICE</div>
      <div class="metadata">
        <div>Invoice #: ${escapeHtml(data.invoiceNumber)}</div>
        <div>Issue Date: ${escapeHtml(data.issueDate)}</div>
        <div>Due Date: ${escapeHtml(data.dueDate)}</div>
      </div>
      ${renderStatusMarker(data.statusLabel)}
    </div>
  </div>

  <div class="bill-to">
    <h2>BILL TO</h2>
    <div>${escapeHtml(data.customer?.displayName)}</div>
    ${renderLines(data.customer?.addressLines)}
    ${data.customer?.email ? `<div>${escapeHtml(data.customer.email)}</div>` : ""}
    ${data.customer?.taxId ? `<div>Tax ID: ${escapeHtml(data.customer.taxId)}</div>` : ""}
  </div>

  <table>
    <thead>
      <tr>
        <th>Description</th>
        <th class="num">Qty</th>
        <th class="num">Unit Price</th>
        ${showVAT ? '<th class="num">VAT</th>' : ""}
        ${showVAT ? '<th class="num">VAT Amt</th>' : ""}
        <th class="num">Total</th>
      </tr>
    </thead>
    <tbody>
      ${renderLineRows(data.lines, showVAT)}
    </tbody>
  </table>

  <div class="totals">
    ${renderTotalsRows(data)}
  </div>

  ${
    data.notes
      ? `<div class="notes"><h2>Notes</h2><div>${escapeHtml(data.notes).replaceAll("\n", "<br/>")}</div></div>`
      : ""
  }
</body>
</html>`;
}
