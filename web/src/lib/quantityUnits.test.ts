import { describe, expect, it } from "vitest";
import { quantityUnitError, quantityUnitOptions } from "./quantityUnits";

const defaults = ["Day(s)", "Hour(s)", "KG", "g", "Pcs"];

describe("quantityUnitOptions", () => {
  it("is the settings list when the current unit is in it, or blank", () => {
    expect(quantityUnitOptions(defaults, "KG")).toEqual(defaults);
    expect(quantityUnitOptions(defaults, "")).toEqual(defaults);
  });

  it("adds a current unit that is no longer in the list", () => {
    expect(quantityUnitOptions(defaults, "Pallets")).toEqual([...defaults, "Pallets"]);
  });
});

describe("quantityUnitError", () => {
  it("accepts a new unit", () => {
    expect(quantityUnitError(defaults, "  Pallets ")).toBeNull();
  });

  it("rejects blank, too long, duplicate and over-the-limit units", () => {
    expect(quantityUnitError(defaults, "   ")).toBe("Enter a unit.");
    expect(quantityUnitError(defaults, "x".repeat(31))).toMatch(/at most 30/);
    expect(quantityUnitError(defaults, "kg")).toBe("That unit is already in the list.");
    expect(quantityUnitError(Array.from({ length: 50 }, (_, i) => `U${i}`), "New")).toMatch(/At most 50/);
  });
});
