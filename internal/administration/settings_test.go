package admin

import "testing"

func TestSettings_NextInvoiceNumber(t *testing.T) {
	s := &Settings{InvoiceNumber: 0}

	if got := s.NextInvoiceNumber(); got != 1 {
		t.Errorf("expected first call to return 1, got %d", got)
	}

	if s.InvoiceNumber != 1 {
		t.Errorf("expected InvoiceNumber to be 1 after the call, got %d", s.InvoiceNumber)
	}

	if got := s.NextInvoiceNumber(); got != 2 {
		t.Errorf("expected second call to return 2, got %d", got)
	}

	if got := s.NextInvoiceNumber(); got != 3 {
		t.Errorf("expected third call to return 3, got %d", got)
	}
}

func TestSettings_NextInvoiceNumber_FromNonZeroStart(t *testing.T) {
	s := &Settings{InvoiceNumber: 10}

	if got := s.NextInvoiceNumber(); got != 11 {
		t.Errorf("expected 11, got %d", got)
	}

	if s.InvoiceNumber != 11 {
		t.Errorf("expected InvoiceNumber to be 11, got %d", s.InvoiceNumber)
	}
}
