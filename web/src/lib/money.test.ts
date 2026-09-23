import { describe, expect, it } from "vitest";
import {
  currencyExponent,
  formatMoney,
  minorToInputString,
  parseMoneyInput,
  previewLineSubtotal,
  previewLineVat,
} from "./money";

describe("currencyExponent", () => {
  it("defaults to 2 decimal places", () => {
    expect(currencyExponent("GBP")).toBe(2);
    expect(currencyExponent("usd")).toBe(2);
  });

  it("knows zero-decimal currencies", () => {
    expect(currencyExponent("JPY")).toBe(0);
  });

  it("knows three-decimal currencies", () => {
    expect(currencyExponent("KWD")).toBe(3);
  });
});

describe("formatMoney", () => {
  it("formats GBP minor units as pounds and pence", () => {
    expect(formatMoney(12345, "GBP")).toBe("£123.45");
  });

  it("formats a zero-decimal currency with no decimal places", () => {
    // CLDR's en-GB symbol for JPY is "JP¥" (disambiguated from CNY's "¥").
    expect(formatMoney(500, "JPY")).toBe("JP¥500");
  });

  it("still renders an amount for a syntactically valid but unlisted currency code", () => {
    // ECMA-402 only requires the currency code look like ISO 4217
    // (3 letters); Intl renders an unrecognised-but-valid-shaped code
    // using the code itself in place of a symbol, rather than throwing.
    // (Intl inserts a non-breaking space here, hence the \s match rather
    // than an exact string.)
    expect(formatMoney(1000, "XYZ")).toMatch(/^XYZ\s10\.00$/);
  });

  it("falls back to a plain, non-throwing format for a currency value Intl actually rejects", () => {
    expect(formatMoney(1000, "TOOLONG")).toBe("10.00 TOOLONG");
  });
});

describe("parseMoneyInput", () => {
  it("parses a plain decimal amount into integer minor units", () => {
    expect(parseMoneyInput("123.45", "GBP")).toBe(12345);
  });

  it("parses a whole-number amount", () => {
    expect(parseMoneyInput("50", "GBP")).toBe(5000);
  });

  it("rejects more precision than the currency supports", () => {
    expect(parseMoneyInput("1.234", "GBP")).toBeNull();
  });

  it("rejects negative amounts", () => {
    expect(parseMoneyInput("-5", "GBP")).toBeNull();
  });

  it("rejects non-numeric input", () => {
    expect(parseMoneyInput("abc", "GBP")).toBeNull();
  });

  it("handles a zero-decimal currency correctly", () => {
    expect(parseMoneyInput("500", "JPY")).toBe(500);
  });

  it("never produces a float via multiplication for a value that would round incorrectly", () => {
    // 0.1 + 0.2 === 0.30000000000000004 in naive float math; string-based
    // parsing must not be affected by that class of bug.
    expect(parseMoneyInput("0.30", "GBP")).toBe(30);
  });
});

describe("minorToInputString", () => {
  it("round-trips through parseMoneyInput", () => {
    expect(minorToInputString(12345, "GBP")).toBe("123.45");
    expect(parseMoneyInput(minorToInputString(12345, "GBP"), "GBP")).toBe(12345);
  });

  it("pads small amounts correctly", () => {
    expect(minorToInputString(5, "GBP")).toBe("0.05");
  });

  it("handles a zero-decimal currency", () => {
    expect(minorToInputString(500, "JPY")).toBe("500");
  });
});

describe("previewLineSubtotal / previewLineVat", () => {
  it("computes a whole-quantity line total", () => {
    expect(previewLineSubtotal(2, 50000)).toBe(100000);
  });

  it("computes a fractional-quantity line total (e.g. hours)", () => {
    expect(previewLineSubtotal(2.5, 10000)).toBe(25000);
  });

  it("rounds to the nearest whole minor unit", () => {
    expect(previewLineSubtotal(3, 333)).toBe(999);
  });

  it("computes VAT at a given percentage", () => {
    expect(previewLineVat(100000, 20)).toBe(20000);
  });

  it("returns 0 for non-finite input rather than NaN", () => {
    expect(previewLineSubtotal(Number.NaN, 100)).toBe(0);
  });
});
