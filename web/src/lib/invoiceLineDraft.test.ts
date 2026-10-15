import { describe, expect, it } from "vitest";
import {
  hasLineDraftErrors,
  lineDefaultsFromProduct,
  newLineDraft,
  previewInvoiceTotals,
  previewLineTotal,
  toCreateInvoiceLine,
  validateLineDraft,
  withoutVat,
  type LineDraft,
} from "./invoiceLineDraft";

function makeLine(overrides: Partial<LineDraft> = {}): LineDraft {
  return { ...newLineDraft(), description: "Consulting", quantity: "2", unitPrice: "500.00", vatRate: "20", ...overrides };
}

describe("validateLineDraft", () => {
  it("accepts a fully valid line", () => {
    expect(hasLineDraftErrors(validateLineDraft(makeLine(), "GBP"))).toBe(false);
  });

  it("requires a description", () => {
    const errors = validateLineDraft(makeLine({ description: "  " }), "GBP");
    expect(errors.description).toBeDefined();
  });

  it("rejects a zero or negative quantity", () => {
    expect(validateLineDraft(makeLine({ quantity: "0" }), "GBP").quantity).toBeDefined();
    expect(validateLineDraft(makeLine({ quantity: "-1" }), "GBP").quantity).toBeDefined();
  });

  it("accepts a fractional quantity", () => {
    expect(validateLineDraft(makeLine({ quantity: "2.5" }), "GBP").quantity).toBeUndefined();
  });

  it("rejects an unparseable unit price", () => {
    expect(validateLineDraft(makeLine({ unitPrice: "abc" }), "GBP").unitPrice).toBeDefined();
  });

  it("rejects a negative VAT rate", () => {
    expect(validateLineDraft(makeLine({ vatRate: "-5" }), "GBP").vatRate).toBeDefined();
  });

  it("accepts a zero VAT rate", () => {
    expect(validateLineDraft(makeLine({ vatRate: "0" }), "GBP").vatRate).toBeUndefined();
  });
});

describe("previewLineTotal / previewInvoiceTotals", () => {
  it("computes subtotal, VAT, and total for one line", () => {
    const result = previewLineTotal(makeLine({ quantity: "2", unitPrice: "500.00", vatRate: "20" }), "GBP");
    expect(result).toEqual({ subtotal: 100000, vat: 20000, total: 120000 });
  });

  it("sums multiple lines", () => {
    const lines = [
      makeLine({ quantity: "1", unitPrice: "100.00", vatRate: "0" }),
      makeLine({ quantity: "2", unitPrice: "50.00", vatRate: "20" }),
    ];
    expect(previewInvoiceTotals(lines, "GBP")).toEqual({ subtotal: 20000, vatTotal: 2000, total: 22000 });
  });

  it("treats an invalid numeric field as 0 for preview purposes only", () => {
    const result = previewLineTotal(makeLine({ quantity: "not-a-number" }), "GBP");
    expect(result).toEqual({ subtotal: 0, vat: 0, total: 0 });
  });
});

describe("toCreateInvoiceLine", () => {
  it("serializes a line into the API request shape with integer minor units", () => {
    const line = makeLine({ productId: "prod-1", quantity: "2.5", unitPrice: "10.00", vatRate: "20" });
    expect(toCreateInvoiceLine(line, "GBP")).toEqual({
      productId: "prod-1",
      description: "Consulting",
      quantity: 2.5,
      unitPrice: 1000,
      vatRate: 20,
    });
  });

  it("omits productId when none was selected", () => {
    const line = makeLine({ productId: "" });
    expect(toCreateInvoiceLine(line, "GBP").productId).toBeUndefined();
  });
});

describe("withoutVat", () => {
  it("forces every line to a 0% VAT rate, leaving everything else alone", () => {
    const line = makeLine({ vatRate: "20" });
    const [stripped] = withoutVat([line]);
    expect(stripped).toEqual({ ...line, vatRate: "0" });
    expect(previewInvoiceTotals([stripped], "GBP").vatTotal).toBe(0);
    expect(toCreateInvoiceLine(stripped, "GBP").vatRate).toBe(0);
  });

  it("ignores an invalid VAT rate left in a hidden field", () => {
    const [stripped] = withoutVat([makeLine({ vatRate: "-5" })]);
    expect(validateLineDraft(stripped, "GBP").vatRate).toBeUndefined();
  });
});

describe("toCreateInvoiceLine quantity unit", () => {
  it("sends the unit, and omits it when the line has none", () => {
    expect(toCreateInvoiceLine(makeLine({ quantityUnit: "Hour(s)" }), "GBP").quantityUnit).toBe("Hour(s)");
    expect(toCreateInvoiceLine(makeLine({ quantityUnit: "" }), "GBP").quantityUnit).toBeUndefined();
  });
});

describe("lineDefaultsFromProduct", () => {
  const product = {
    id: "66666666-6666-6666-6666-666666666666",
    organisationId: "7c9e6679-7425-40de-944b-e07fc1f90ae7",
    name: "Consulting (half day)",
    sku: "CONS-HD",
    price: 50000,
    vatRate: 20,
    isActive: true,
    createdAt: "2026-01-15T10:00:00Z",
    updatedAt: "2026-01-15T10:00:00Z",
  };

  it("fills the description, unit price and VAT % from the product", () => {
    expect(lineDefaultsFromProduct(product, "GBP", true)).toEqual({
      description: "Consulting (half day)",
      unitPrice: "500.00",
      vatRate: "20",
    });
  });

  it("fills the quantity unit when the product has one, and leaves it alone otherwise", () => {
    expect(lineDefaultsFromProduct({ ...product, quantityUnit: "Day(s)" }, "GBP", true)).toHaveProperty(
      "quantityUnit",
      "Day(s)",
    );
    expect(lineDefaultsFromProduct(product, "GBP", true)).not.toHaveProperty("quantityUnit");
  });

  it("leaves the VAT % alone when the product has no default", () => {
    const { vatRate: _vatRate, ...noVat } = product;
    expect(lineDefaultsFromProduct(noVat, "GBP", true)).not.toHaveProperty("vatRate");
  });

  it("never sets a VAT % for an organisation that isn't VAT registered", () => {
    expect(lineDefaultsFromProduct(product, "GBP", false)).not.toHaveProperty("vatRate");
  });

  it("keeps a 0% default rather than treating it as no default", () => {
    expect(lineDefaultsFromProduct({ ...product, vatRate: 0 }, "GBP", true)).toHaveProperty("vatRate", "0");
  });
});
