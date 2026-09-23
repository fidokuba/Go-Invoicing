import { describe, expect, it } from "vitest";
import { canRecordPayment, canSendInvoice, isFinancialContentImmutable } from "./invoiceLifecycle";
import type { InvoiceStatus } from "@/components/invoice/InvoiceStatusBadge";

const ALL_STATUSES: InvoiceStatus[] = ["draft", "sent", "overdue", "paid"];

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
