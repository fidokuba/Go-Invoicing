import { describe, expect, it } from "vitest";
import { canCancelInvoice, canRecordPayment, canSendInvoice, isFinancialContentImmutable } from "./invoiceLifecycle";
import type { InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";

const ALL_STATUSES: InvoiceStatus[] = ["draft", "sent", "overdue", "paid", "cancelled"];

describe("canSendInvoice", () => {
  it("is only true for draft", () => {
    expect(ALL_STATUSES.filter(canSendInvoice)).toEqual(["draft"]);
  });
});

describe("canRecordPayment", () => {
  it("is true for sent and overdue only", () => {
    expect(ALL_STATUSES.filter(canRecordPayment)).toEqual(["sent", "overdue"]);
  });

  it("is false for draft (backend also returns 409)", () => {
    expect(canRecordPayment("draft")).toBe(false);
  });

  it("is false for paid (already settled)", () => {
    expect(canRecordPayment("paid")).toBe(false);
  });
});

describe("isFinancialContentImmutable", () => {
  it("is false only for draft", () => {
    expect(isFinancialContentImmutable("draft")).toBe(false);
    expect(isFinancialContentImmutable("sent")).toBe(true);
    expect(isFinancialContentImmutable("overdue")).toBe(true);
    expect(isFinancialContentImmutable("paid")).toBe(true);
  });
});

describe("canCancelInvoice", () => {
  it("is true for unpaid draft, sent and overdue invoices", () => {
    expect(canCancelInvoice("draft", 0)).toBe(true);
    expect(canCancelInvoice("sent", 0)).toBe(true);
    expect(canCancelInvoice("overdue", 0)).toBe(true);
  });

  it("is false once anything has been paid", () => {
    expect(canCancelInvoice("sent", 1)).toBe(false);
    expect(canCancelInvoice("overdue", 500)).toBe(false);
  });

  it("is false for paid and already-cancelled invoices", () => {
    expect(canCancelInvoice("paid", 10000)).toBe(false);
    expect(canCancelInvoice("cancelled", 0)).toBe(false);
  });
});
