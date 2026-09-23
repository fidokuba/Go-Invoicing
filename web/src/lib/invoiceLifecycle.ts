import type { InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";

/**
 * Pure lifecycle/visibility rules (Milestone 12 section 13/21) — used
 * only to decide which *actions to show*; the backend independently
 * re-enforces every one of these on the actual request (Send returns
 * 409 on an already-Sent/Paid invoice, payments return 409 on a Draft
 * invoice or an amount exceeding the outstanding balance — see
 * api/openapi.yaml). Hiding a button here is a UX convenience, never the
 * security boundary.
 */
export function canSendInvoice(status: InvoiceStatus): boolean {
  return status === "draft";
}

export function canRecordPayment(status: InvoiceStatus): boolean {
  return status === "sent" || status === "overdue";
}

/** Whether the invoice's financial content (lines, totals) is still
 * editable — only ever true for a Draft; Send captures an immutable
 * snapshot of everything else. This app has no line-editing UI post
 * creation regardless (invoices are created whole), but this stays the
 * one place that fact is asserted, for InvoiceDetailPage and its tests. */
export function isFinancialContentImmutable(status: InvoiceStatus): boolean {
  return status !== "draft";
}
