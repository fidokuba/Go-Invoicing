package invoice

import "testing"

func TestCalculateLineAmounts(t *testing.T) {
	tests := []struct {
		name          string
		quantity      float64
		unitPrice     int64
		vatRate       float64
		wantVATAmount int64
		wantTotal     int64
	}{
		{
			name:          "whole number quantity, exact VAT",
			quantity:      2,
			unitPrice:     1000,
			vatRate:       20,
			wantVATAmount: 400, // 2*1000=2000 subtotal; 2000*20/100=400
			wantTotal:     2400,
		},
		{
			name:          "fractional quantity 1.5, exact subtotal",
			quantity:      1.5,
			unitPrice:     1000,
			vatRate:       20,
			wantVATAmount: 300, // 1.5*1000=1500 subtotal; 1500*20/100=300
			wantTotal:     1800,
		},
		{
			name:          "fractional quantity requiring subtotal rounding",
			quantity:      1.5,
			unitPrice:     333,
			vatRate:       20,
			wantVATAmount: 100, // 1.5*333=499.5 -> rounds to 500; 500*20/100=100
			wantTotal:     600,
		},
		{
			name:          "small fractional quantity 0.5",
			quantity:      0.5,
			unitPrice:     999,
			vatRate:       0,
			wantVATAmount: 0, // 0.5*999=499.5 -> rounds to 500; VAT is 0
			wantTotal:     500,
		},
		{
			name:          "VAT rounding exact half rounds away from zero",
			quantity:      1,
			unitPrice:     1000,
			vatRate:       12.55,
			wantVATAmount: 126, // 1000*12.55/100=125.5 -> rounds to 126
			wantTotal:     1126,
		},
		{
			name:          "VAT rounding non-tie fraction",
			quantity:      1,
			unitPrice:     999,
			vatRate:       17.5,
			wantVATAmount: 175, // 999*17.5/100=174.825 -> rounds to 175
			wantTotal:     1174,
		},
		{
			name:          "zero VAT rate",
			quantity:      3,
			unitPrice:     500,
			vatRate:       0,
			wantVATAmount: 0,
			wantTotal:     1500,
		},
		{
			name:          "fractional quantity 2.25",
			quantity:      2.25,
			unitPrice:     400,
			vatRate:       10,
			wantVATAmount: 90, // 2.25*400=900 subtotal; 900*10/100=90
			wantTotal:     990,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vatAmount, total := calculateLineAmounts(tt.quantity, tt.unitPrice, tt.vatRate)

			if vatAmount != tt.wantVATAmount {
				t.Errorf("vatAmount: got %d, want %d", vatAmount, tt.wantVATAmount)
			}

			if total != tt.wantTotal {
				t.Errorf("total: got %d, want %d", total, tt.wantTotal)
			}

			// Invariant: lineSubtotal (total - vatAmount) must always be
			// recoverable and non-negative for valid (non-negative) inputs.
			if total-vatAmount < 0 {
				t.Errorf("recovered lineSubtotal is negative: total=%d vatAmount=%d", total, vatAmount)
			}
		})
	}
}

func TestSumInvoiceTotals(t *testing.T) {
	lines := []*Line{
		{VATAmount: 400, Total: 2400}, // subtotal 2000
		{VATAmount: 300, Total: 1800}, // subtotal 1500
		{VATAmount: 0, Total: 500},    // subtotal 500
		{VATAmount: 90, Total: 990},   // subtotal 900
	}

	subtotal, vatTotal, total := sumInvoiceTotals(lines)

	wantSubtotal := int64(2000 + 1500 + 500 + 900)
	wantVATTotal := int64(400 + 300 + 0 + 90)
	wantTotal := int64(2400 + 1800 + 500 + 990)

	if subtotal != wantSubtotal {
		t.Errorf("subtotal: got %d, want %d", subtotal, wantSubtotal)
	}

	if vatTotal != wantVATTotal {
		t.Errorf("vatTotal: got %d, want %d", vatTotal, wantVATTotal)
	}

	if total != wantTotal {
		t.Errorf("total: got %d, want %d", total, wantTotal)
	}

	// Invariant required by the spec: total == subtotal + vatTotal.
	if total != subtotal+vatTotal {
		t.Errorf("total (%d) does not equal subtotal+vatTotal (%d)", total, subtotal+vatTotal)
	}
}

func TestSumInvoiceTotals_SingleLine(t *testing.T) {
	lines := []*Line{
		{VATAmount: 175, Total: 1174},
	}

	subtotal, vatTotal, total := sumInvoiceTotals(lines)

	if subtotal != 999 {
		t.Errorf("subtotal: got %d, want %d", subtotal, 999)
	}

	if vatTotal != 175 {
		t.Errorf("vatTotal: got %d, want %d", vatTotal, 175)
	}

	if total != 1174 {
		t.Errorf("total: got %d, want %d", total, 1174)
	}
}
