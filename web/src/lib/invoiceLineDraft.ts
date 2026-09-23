import { parseMoneyInput, previewLineSubtotal, previewLineVat } from "./money";
import type { components } from "@/api/schema";

type CreateInvoiceLineRequest = components["schemas"]["CreateInvoiceLineRequest"];

/**
 * The invoice editor's in-progress representation of one line (Milestone
 * 12 section 11) — every numeric field is kept as its raw text input
 * (not a parsed number) so a user can type "1." or clear the field
 * entirely without the input fighting them; parsing/validation happens
 * only at submit time (validateLineDraft) and for the live preview
 * (previewLineTotal).
 */
export interface LineDraft {
  key: string;
  productId: string;
  description: string;
  quantity: string;
  unitPrice: string;
  vatRate: string;
}

let lineKeySeq = 0;
export function newLineDraft(): LineDraft {
  lineKeySeq += 1;
  return {
    key: `line-${lineKeySeq}`,
    productId: "",
    description: "",
    quantity: "1",
    unitPrice: "0.00",
    vatRate: "0",
  };
}

export interface LineDraftErrors {
  description?: string;
  quantity?: string;
  unitPrice?: string;
  vatRate?: string;
}

export function validateLineDraft(line: LineDraft, currency: string): LineDraftErrors {
  const errors: LineDraftErrors = {};

  if (!line.description.trim()) errors.description = "Required";

  const quantity = Number(line.quantity);
  if (!Number.isFinite(quantity) || quantity <= 0) errors.quantity = "Must be greater than 0";

  if (parseMoneyInput(line.unitPrice, currency) === null) errors.unitPrice = "Invalid amount";

  const vatRate = Number(line.vatRate);
  if (!Number.isFinite(vatRate) || vatRate < 0) errors.vatRate = "Must be 0 or more";

  return errors;
}

export function hasLineDraftErrors(errors: LineDraftErrors): boolean {
  return Object.keys(errors).length > 0;
}

/** Preview-only subtotal+VAT for one line, in integer minor units — see
 * money.ts's own comment on why this is presentation-only. */
export function previewLineTotal(line: LineDraft, currency: string): { subtotal: number; vat: number; total: number } {
  const quantity = Number(line.quantity) || 0;
  const unitPriceMinor = parseMoneyInput(line.unitPrice, currency) ?? 0;
  const vatRate = Number(line.vatRate) || 0;

  const subtotal = previewLineSubtotal(quantity, unitPriceMinor);
  const vat = previewLineVat(subtotal, vatRate);
  return { subtotal, vat, total: subtotal + vat };
}

export function previewInvoiceTotals(lines: LineDraft[], currency: string) {
  return lines.reduce(
    (acc, line) => {
      const { subtotal, vat } = previewLineTotal(line, currency);
      return { subtotal: acc.subtotal + subtotal, vatTotal: acc.vatTotal + vat, total: acc.total + subtotal + vat };
    },
    { subtotal: 0, vatTotal: 0, total: 0 },
  );
}

/** Serializes one line draft into exactly the shape POST /api/v1/invoices
 * expects (Milestone 12 section 12) — unitPrice is always converted from
 * the user's decimal text via integer-minor-unit string arithmetic
 * (parseMoneyInput), never a float multiplication. Must only be called
 * after validateLineDraft reports no errors. */
export function toCreateInvoiceLine(line: LineDraft, currency: string): CreateInvoiceLineRequest {
  return {
    productId: line.productId || undefined,
    description: line.description,
    quantity: Number(line.quantity),
    unitPrice: parseMoneyInput(line.unitPrice, currency) ?? 0,
    vatRate: Number(line.vatRate),
  };
}
