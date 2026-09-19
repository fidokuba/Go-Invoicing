package invoice

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// currencySymbols covers the handful of ordinary 2-decimal currencies this
// project actually needs a real symbol for. Any other 3-letter code still
// formats correctly (see FormatMoney) — it just prints the code instead
// of inventing a symbol, which is honest rather than guessing wrong.
var currencySymbols = map[string]string{
	"GBP": "£",
	"EUR": "€",
	"USD": "$",
}

// FormatMoney formats minorUnits (always an integer — never a float, per
// this project's money convention) as a human-readable amount in
// currency. A known currency gets its symbol prefixed directly (£123.45);
// an unrecognised 3-letter code gets its amount followed by the code
// itself (123.45 XYZ) rather than a fabricated or wrong symbol. Negative
// amounts place the sign before the symbol/amount (-£1.23), matching how
// a plain minus-prefixed number reads.
//
// This never converts minorUnits to a float — major/minor units are
// computed with plain integer division and modulo, so there is no
// floating-point rounding risk at any amount. The absolute value is
// computed via absUint64 rather than a naive "if n < 0 { n = -n }": that
// naive form overflows silently for math.MinInt64 (its negation isn't
// representable as an int64 and wraps back to itself, still negative),
// which would print a garbled, still-negative amount instead of the
// correct magnitude. No invoice will ever realistically reach anywhere
// near that magnitude, but the function is correct at every representable
// int64 value regardless, not just the ones this domain currently
// produces.
func FormatMoney(minorUnits int64, currency string) string {
	negative := minorUnits < 0
	abs := absUint64(minorUnits)

	major := abs / 100
	minor := abs % 100
	amount := fmt.Sprintf("%d.%02d", major, minor)

	sign := ""
	if negative {
		sign = "-"
	}

	code := strings.ToUpper(strings.TrimSpace(currency))
	if symbol, ok := currencySymbols[code]; ok {
		return sign + symbol + amount
	}

	return sign + amount + " " + code
}

// absUint64 returns the absolute value of n as a uint64, correct for
// every int64 value including math.MinInt64 — whose magnitude
// (9223372036854775808) doesn't fit in an int64 at all, only in a
// uint64. Negating n directly (int64(-n)) would overflow for that one
// value; adding 1 to n before negating keeps the intermediate value
// within int64's representable range (MinInt64+1 is exactly -MaxInt64),
// and the "+1" is restored afterward once the result is safely in
// uint64's much larger range.
func absUint64(n int64) uint64 {
	if n < 0 {
		return uint64(-(n + 1)) + 1
	}

	return uint64(n)
}

// FormatQuantity formats a line quantity without meaningless trailing
// zeros: 1 stays "1", 1.5 stays "1.5", 2.25 stays "2.25" — never
// "1.000000". strconv.FormatFloat's -1 precision means "the shortest
// decimal representation that round-trips exactly back to this float64",
// which is exactly this behaviour, with no bespoke trimming logic needed.
func FormatQuantity(quantity float64) string {
	return strconv.FormatFloat(quantity, 'f', -1, 64)
}

// FormatVATRate formats a VAT percentage the same way FormatQuantity
// formats a quantity (0 -> "0%", 20 -> "20%", 12.5 -> "12.5%"), since
// VATRate is stored as the same float64 shape and deserves the same
// no-trailing-zeros treatment.
func FormatVATRate(rate float64) string {
	return strconv.FormatFloat(rate, 'f', -1, 64) + "%"
}

// invoiceDateLayout renders IssueDate/DueDate as "02 Jan 2026" — a
// clear, unambiguous, professional format for a printed document.
// Deliberately not RFC3339: a raw timestamp has no place on an invoice a
// human is meant to read.
const invoiceDateLayout = "02 Jan 2006"

// FormatInvoiceDate formats a date for display on the PDF.
func FormatInvoiceDate(t time.Time) string {
	return t.Format(invoiceDateLayout)
}

// filenameUnsafeChars matches everything outside the conservative
// A-Z/a-z/0-9/-/_ set — this is a strict allow-list, not a blocklist, so
// there is no way for a quote, CR, LF, semicolon, path separator, or
// anything else to survive into a Content-Disposition header.
var filenameUnsafeChars = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// SanitizeFilenameComponent strips every character outside the
// conservative safe set from value, for safe interpolation into a
// Content-Disposition filename. An invoice number is server-generated
// and already safe by construction (see Settings.InvoicePrefix +
// sequential number), but this sanitizes defensively regardless — the
// same principle as never trusting a value merely because "it shouldn't
// contain anything unsafe." A result that becomes empty after
// sanitization (e.g. an invoice number that was somehow all unsafe
// characters) falls back to "invoice" rather than producing a filename
// of "invoice-.pdf" or worse, an empty component.
func SanitizeFilenameComponent(value string) string {
	sanitized := filenameUnsafeChars.ReplaceAllString(value, "")
	if sanitized == "" {
		return "invoice"
	}

	return sanitized
}
