/**
 * Date/time helpers (Milestone 12 section 29).
 *
 * The API distinguishes two shapes (api/openapi.yaml, "Dates and
 * times"): a calendar date (issueDate, dueDate, paymentDate) is
 * `YYYY-MM-DD` with no time-of-day or zone; a timestamp (createdAt,
 * sentAt, ...) is RFC 3339 UTC. These need different handling —
 * `new Date("2026-09-19")` parses as UTC midnight, and formatting that
 * with the browser's local timezone can silently roll it back a day for
 * any viewer west of UTC. formatDateOnly avoids that entirely by never
 * parsing the string through the UTC-string Date constructor.
 */

/** Format a date-only "YYYY-MM-DD" string for display, with no
 * timezone-shift risk — e.g. "2026-09-19" -> "19 Sep 2026". */
export function formatDateOnly(dateOnly: string): string {
  const parts = dateOnly.split("-").map(Number);
  if (parts.length !== 3 || parts.some(Number.isNaN)) return dateOnly;
  const [year, month, day] = parts;
  // The local-time Date constructor (y, m, d, ...) — as opposed to
  // parsing an ISO string, which Date always treats as UTC — just
  // re-labels the same calendar date in local time, with no conversion
  // at all.
  const date = new Date(year, month - 1, day);
  return new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  }).format(date);
}

/** Today's date as "YYYY-MM-DD" in the viewer's local calendar — used as
 * a default for date inputs (issueDate, paymentDate). */
export function todayDateOnly(): string {
  const now = new Date();
  const y = now.getFullYear();
  const m = String(now.getMonth() + 1).padStart(2, "0");
  const d = String(now.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** Add `days` calendar days to a "YYYY-MM-DD" date-only string, returning
 * the same format — used to default an invoice's due date from the
 * organisation's payment-terms setting. Pure calendar-day arithmetic; no
 * timezone involved since neither the input nor output has a
 * time-of-day. */
export function addDaysDateOnly(dateOnly: string, days: number): string {
  const [year, month, day] = dateOnly.split("-").map(Number);
  const date = new Date(year, month - 1, day + days);
  const y = date.getFullYear();
  const m = String(date.getMonth() + 1).padStart(2, "0");
  const d = String(date.getDate()).padStart(2, "0");
  return `${y}-${m}-${d}`;
}

/** Format an RFC 3339 UTC timestamp as just its local calendar date
 * (e.g. for a "Created" table column) — unlike formatDateOnly, this
 * DOES convert to local time first, because a timestamp names a real
 * instant (a viewer far enough from UTC may see it land on a different
 * local calendar day than its UTC date part, which is correct here). */
export function formatTimestampDate(isoTimestamp: string): string {
  const date = new Date(isoTimestamp);
  if (Number.isNaN(date.getTime())) return isoTimestamp;
  return new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
  }).format(date);
}

/** Format an RFC 3339 UTC timestamp for display, converted to the
 * viewer's local time (unlike date-only values, a timestamp names a real
 * instant, so local-time conversion is the correct, expected
 * behaviour). */
export function formatTimestamp(isoTimestamp: string): string {
  const date = new Date(isoTimestamp);
  if (Number.isNaN(date.getTime())) return isoTimestamp;
  return new Intl.DateTimeFormat("en-GB", {
    day: "numeric",
    month: "short",
    year: "numeric",
    hour: "2-digit",
    minute: "2-digit",
  }).format(date);
}
