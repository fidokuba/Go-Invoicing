package invoice

import (
	"testing"
	"time"
)

func TestFormatMoney(t *testing.T) {
	tests := []struct {
		name       string
		minorUnits int64
		currency   string
		want       string
	}{
		{"zero GBP", 0, "GBP", "£0.00"},
		{"ordinary GBP", 12345, "GBP", "£123.45"},
		{"ordinary EUR pennies", 100, "EUR", "€1.00"},
		{"ordinary USD zero", 0, "USD", "$0.00"},
		{"single penny GBP", 1, "GBP", "£0.01"},
		{"negative GBP", -123, "GBP", "-£1.23"},
		{"negative large", -999999, "USD", "-$9999.99"},
		{"unknown currency", 12345, "XYZ", "123.45 XYZ"},
		{"unknown currency lowercase input", 12345, "xyz", "123.45 XYZ"},
		{"negative unknown currency", -100, "XYZ", "-1.00 XYZ"},
		{"lowercase known currency normalizes", 12345, "gbp", "£123.45"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := FormatMoney(tt.minorUnits, tt.currency); got != tt.want {
				t.Errorf("FormatMoney(%d, %q) = %q, want %q", tt.minorUnits, tt.currency, got, tt.want)
			}
		})
	}
}

func TestFormatQuantity(t *testing.T) {
	tests := []struct {
		quantity float64
		want     string
	}{
		{1, "1"},
		{1.5, "1.5"},
		{2.25, "2.25"},
		{0.5, "0.5"},
		{100, "100"},
		{3.333, "3.333"},
	}

	for _, tt := range tests {
		got := FormatQuantity(tt.quantity)
		if got != tt.want {
			t.Errorf("FormatQuantity(%v) = %q, want %q", tt.quantity, got, tt.want)
		}
	}
}

func TestFormatVATRate(t *testing.T) {
	tests := []struct {
		rate float64
		want string
	}{
		{0, "0%"},
		{20, "20%"},
		{5, "5%"},
		{12.5, "12.5%"},
	}

	for _, tt := range tests {
		got := FormatVATRate(tt.rate)
		if got != tt.want {
			t.Errorf("FormatVATRate(%v) = %q, want %q", tt.rate, got, tt.want)
		}
	}
}

func TestFormatInvoiceDate(t *testing.T) {
	got := FormatInvoiceDate(time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC))
	want := "02 Jan 2026"
	if got != want {
		t.Errorf("FormatInvoiceDate() = %q, want %q", got, want)
	}
}

func TestSanitizeFilenameComponent(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{"normal invoice number", "INV-123", "INV-123"},
		{"spaces removed", "INV 123", "INV123"},
		{"double quotes removed", `INV"123`, "INV123"},
		{"forward slash removed", "INV/123", "INV123"},
		{"backslash removed", `INV\123`, "INV123"},
		{"CRLF removed", "INV\r\n123", "INV123"},
		{"semicolon removed", "INV;123", "INV123"},
		{"punctuation removed", "INV.123!", "INV123"},
		{"empty falls back", "", "invoice"},
		{"entirely unsafe falls back", "///\"\"\"", "invoice"},
		{"underscore preserved", "INV_123", "INV_123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := SanitizeFilenameComponent(tt.value); got != tt.want {
				t.Errorf("SanitizeFilenameComponent(%q) = %q, want %q", tt.value, got, tt.want)
			}
		})
	}
}
