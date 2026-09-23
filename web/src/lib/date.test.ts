import { describe, expect, it } from "vitest";
import { addDaysDateOnly, formatDateOnly, formatTimestamp, formatTimestampDate, todayDateOnly } from "./date";

describe("formatDateOnly", () => {
  it("formats a date-only string without any timezone shift", () => {
    // The classic bug this guards against: new Date("2026-09-19") parses
    // as UTC midnight, which .toLocaleDateString() in a timezone west of
    // UTC would render as 18 Sep. formatDateOnly must never do that.
    // CLDR's en-GB short month name for September is "Sept" (not "Sep").
    expect(formatDateOnly("2026-09-19")).toBe("19 Sept 2026");
  });

  it("handles a year boundary correctly", () => {
    expect(formatDateOnly("2026-01-01")).toBe("1 Jan 2026");
  });

  it("returns the input unchanged if it isn't a valid date-only string", () => {
    expect(formatDateOnly("not-a-date")).toBe("not-a-date");
  });
});

describe("addDaysDateOnly", () => {
  it("adds days within the same month", () => {
    expect(addDaysDateOnly("2026-09-01", 10)).toBe("2026-09-11");
  });

  it("rolls over a month boundary", () => {
    expect(addDaysDateOnly("2026-09-25", 10)).toBe("2026-10-05");
  });

  it("rolls over a year boundary", () => {
    expect(addDaysDateOnly("2026-12-28", 10)).toBe("2027-01-07");
  });

  it("supports zero days (due immediately)", () => {
    expect(addDaysDateOnly("2026-09-19", 0)).toBe("2026-09-19");
  });
});

describe("todayDateOnly", () => {
  it("returns a YYYY-MM-DD string", () => {
    expect(todayDateOnly()).toMatch(/^\d{4}-\d{2}-\d{2}$/);
  });
});

describe("formatTimestamp / formatTimestampDate", () => {
  // Noon UTC, deliberately: converting to local time for any realistic
  // timezone offset (-12..+14) still lands on the same calendar date, so
  // these assertions aren't sensitive to the test runner's TZ.
  const NOON_UTC = "2026-09-19T12:00:00Z";

  it("formats a valid RFC3339 timestamp", () => {
    expect(formatTimestamp(NOON_UTC)).toMatch(/2026/);
  });

  it("returns the input unchanged for an invalid timestamp", () => {
    expect(formatTimestamp("not-a-timestamp")).toBe("not-a-timestamp");
  });

  it("formats just the date portion of a timestamp", () => {
    expect(formatTimestampDate(NOON_UTC)).toMatch(/Sept 2026/);
  });
});
