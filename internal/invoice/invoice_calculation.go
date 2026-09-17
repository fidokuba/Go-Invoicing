package invoice

import "math"

// calculateLineAmounts computes an invoice line's VAT amount and total
// from its quantity, unit price and VAT rate:
//
//	lineSubtotal = quantity * unitPrice
//	vatAmount    = lineSubtotal * vatRate / 100
//	total        = lineSubtotal + vatAmount
//
// lineSubtotal itself is never returned or stored on Line — the
// invoice_lines table has no such column — but it can always be recovered
// exactly afterwards as total - vatAmount, since both are derived from the
// same rounded value.
//
// Every monetary amount is rounded to the nearest whole minor currency
// unit via roundToInt64 (round-half-away-from-zero). Quantity, unit price
// and VAT rate are always non-negative by the time this runs — enforced
// by InvoiceService.Create's validation before this is ever called — so
// this behaves as ordinary "round half up" in practice.
func calculateLineAmounts(quantity float64, unitPrice int64, vatRate float64) (vatAmount int64, total int64) {
	lineSubtotal := roundToInt64(quantity * float64(unitPrice))
	vatAmount = roundToInt64(float64(lineSubtotal) * vatRate / 100)
	total = lineSubtotal + vatAmount
	return vatAmount, total
}

// roundToInt64 rounds a float64 monetary amount to the nearest int64
// minor currency unit, half away from zero.
func roundToInt64(value float64) int64 {
	return int64(math.Round(value))
}

// sumInvoiceTotals adds up already-computed lines into the invoice-level
// subtotal, VAT total and total. Every line's Total and VATAmount are
// already rounded integers, and each line's subtotal is recovered exactly
// as Total - VATAmount, so this is exact integer addition — no further
// rounding occurs here.
func sumInvoiceTotals(lines []*Line) (subtotal int64, vatTotal int64, total int64) {
	for _, l := range lines {
		subtotal += l.Total - l.VATAmount
		vatTotal += l.VATAmount
		total += l.Total
	}

	return subtotal, vatTotal, total
}
