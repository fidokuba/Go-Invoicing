/**
 * Money helpers (Milestone 12 section 12/24).
 *
 * The API's contract (api/openapi.yaml, "Money") is authoritative: every
 * monetary amount is an integer number of the currency's minor units
 * (e.g. GBP 12345 = £123.45) — never a float, and every invoice carries
 * one currency for all of its monetary fields. Every function here that
 * ends up feeding a *stored* amount (i.e. anything sent back to the API)
 * works on integers via string manipulation, never
 * `Math.round(x * 100)` — floating-point multiplication of a decimal
 * user input is exactly the class of bug this file exists to avoid.
 *
 * `previewLineAmount`/`previewInvoiceTotals` are the one deliberate
 * exception: they compute quantity × price and a VAT percentage for
 * on-screen preview only (invoice line quantities are decimal — e.g. 2.5
 * hours — so integer-only arithmetic isn't possible there at all). The
 * backend recomputes and stores the authoritative result; see those
 * functions' own comments.
 */

// Minor-unit exponent per currency, mirroring the well-known ISO 4217
// exceptions (most currencies use 2 decimal places). Extend this table if
// an organisation's currency setting needs one not listed here — an
// unlisted currency safely defaults to 2.
const ZERO_DECIMAL_CURRENCIES = new Set([
  "BIF", "CLP", "DJF", "GNF", "JPY", "KMF", "KRW", "MGA", "PYG", "RWF",
  "UGX", "VND", "VUV", "XAF", "XOF", "XPF",
]);
const THREE_DECIMAL_CURRENCIES = new Set(["BHD", "JOD", "KWD", "OMR", "TND"]);

export function currencyExponent(currency: string): number {
  const code = currency.toUpperCase();
  if (ZERO_DECIMAL_CURRENCIES.has(code)) return 0;
  if (THREE_DECIMAL_CURRENCIES.has(code)) return 3;
  return 2;
}

/** Format an integer minor-unit amount as a localized currency string,
 * e.g. formatMoney(12345, "GBP") -> "£123.45". */
export function formatMoney(amountMinor: number, currency: string): string {
  const exponent = currencyExponent(currency);
  const major = amountMinor / 10 ** exponent;
  try {
    return new Intl.NumberFormat("en-GB", {
      style: "currency",
      currency: currency.toUpperCase(),
      minimumFractionDigits: exponent,
      maximumFractionDigits: exponent,
    }).format(major);
  } catch {
    // An organisation's currency setting is a free-form 3-letter code
    // (see SettingsResponse's own description — "not validated against
    // an actual currency catalogue"), so Intl may reject one that isn't
    // a real ISO 4217 code. Fall back to a plain, still-correct amount
    // rather than letting the whole page crash.
    return `${major.toFixed(exponent)} ${currency.toUpperCase()}`;
  }
}

/** Parse a user-typed decimal amount (e.g. "123.45") into integer minor
 * units for the given currency, using string arithmetic only — never a
 * float multiplication. Returns null for anything that isn't a plain,
 * non-negative decimal number. */
export function parseMoneyInput(input: string, currency: string): number | null {
  const trimmed = input.trim();
  if (!/^\d+(\.\d+)?$/.test(trimmed)) return null;

  const exponent = currencyExponent(currency);
  const [wholePart, fractionPart = ""] = trimmed.split(".");
  if (fractionPart.length > exponent) return null; // more precision than the currency supports

  const paddedFraction = fractionPart.padEnd(exponent, "0");
  const digits = `${wholePart}${paddedFraction}`.replace(/^0+(?=\d)/, "");
  const minor = Number(digits);
  return Number.isSafeInteger(minor) ? minor : null;
}

/** Render an integer minor-unit amount as a plain decimal string for an
 * editable input's default value (e.g. 12345 -> "123.45"), again via
 * string manipulation only. */
export function minorToInputString(amountMinor: number, currency: string): string {
  const exponent = currencyExponent(currency);
  const sign = amountMinor < 0 ? "-" : "";
  const digits = Math.abs(amountMinor).toString().padStart(exponent + 1, "0");
  if (exponent === 0) return `${sign}${digits}`;
  const whole = digits.slice(0, -exponent);
  const fraction = digits.slice(-exponent);
  return `${sign}${whole}.${fraction}`;
}

/**
 * Preview-only: the line total the backend would compute for
 * quantity × unitPrice, rounded to the nearest whole minor unit
 * (round-half-up). This is never sent to the server — invoice creation
 * sends quantity/unitPrice/vatRate and the server computes and returns
 * the authoritative totals (see api/openapi.yaml's InvoiceLineResponse).
 */
export function previewLineSubtotal(quantity: number, unitPriceMinor: number): number {
  if (!Number.isFinite(quantity) || !Number.isFinite(unitPriceMinor)) return 0;
  return Math.round(quantity * unitPriceMinor);
}

/** Preview-only: the VAT amount for a line at vatRate percent. */
export function previewLineVat(subtotalMinor: number, vatRatePercent: number): number {
  if (!Number.isFinite(vatRatePercent)) return 0;
  return Math.round(subtotalMinor * (vatRatePercent / 100));
}
