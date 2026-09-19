package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
)

func intPtr(i int) *int { return &i }

func seedSettings(t *testing.T, repository *fakeSettingsRepository, organisationID uuid.UUID) {
	t.Helper()

	if err := repository.Create(context.Background(), &Settings{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}); err != nil {
		t.Fatalf("seed settings: %v", err)
	}
}

func newTestSettingsService(t *testing.T, organisationID uuid.UUID) (*SettingsService, *fakeSettingsRepository) {
	t.Helper()

	repository := newFakeSettingsRepository()
	seedSettings(t, repository, organisationID)
	return NewSettingsService(repository), repository
}

func TestSettingsService_Get(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	settings, err := service.Get(context.Background(), organisationID)
	if err != nil {
		t.Fatalf("get settings: %v", err)
	}

	if settings.Currency != "GBP" || settings.PaymentTerms != 30 || settings.InvoicePrefix != "INV-" {
		t.Errorf("expected the default seeded settings, got %+v", settings)
	}
}

func TestSettingsService_Update_PartialSemantics(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	// First update: only Currency.
	updated, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{Currency: strPtr("eur")})
	if err != nil {
		t.Fatalf("update currency: %v", err)
	}
	if updated.Currency != "EUR" {
		t.Errorf("expected normalized currency %q, got %q", "EUR", updated.Currency)
	}
	if updated.PaymentTerms != 30 || updated.InvoicePrefix != "INV-" {
		t.Errorf("expected other fields unchanged, got %+v", updated)
	}

	// Second update: only PaymentTerms — Currency from the first update
	// must survive untouched.
	updated, err = service.Update(context.Background(), organisationID, UpdateSettingsRequest{PaymentTerms: intPtr(14)})
	if err != nil {
		t.Fatalf("update payment terms: %v", err)
	}
	if updated.PaymentTerms != 14 {
		t.Errorf("expected payment terms 14, got %d", updated.PaymentTerms)
	}
	if updated.Currency != "EUR" {
		t.Errorf("expected currency to remain %q from the earlier update, got %q", "EUR", updated.Currency)
	}

	// Third update: only InvoicePrefix.
	updated, err = service.Update(context.Background(), organisationID, UpdateSettingsRequest{InvoicePrefix: strPtr("ACME-")})
	if err != nil {
		t.Fatalf("update invoice prefix: %v", err)
	}
	if updated.InvoicePrefix != "ACME-" {
		t.Errorf("expected invoice prefix %q, got %q", "ACME-", updated.InvoicePrefix)
	}
	if updated.Currency != "EUR" || updated.PaymentTerms != 14 {
		t.Errorf("expected earlier updates to survive, got %+v", updated)
	}
}

func TestSettingsService_Update_CurrencyNormalization(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	updated, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{Currency: strPtr("  usd  ")})
	if err != nil {
		t.Fatalf("update currency: %v", err)
	}
	if updated.Currency != "USD" {
		t.Errorf("expected trimmed+uppercased currency %q, got %q", "USD", updated.Currency)
	}
}

func TestSettingsService_Update_InvalidCurrency(t *testing.T) {
	tests := []struct {
		name     string
		currency string
	}{
		{"too short", "EU"},
		{"too long", "EURO"},
		{"contains digits", "US1"},
		{"blank", "   "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			organisationID := uuid.New()
			service, _ := newTestSettingsService(t, organisationID)

			_, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{Currency: strPtr(tt.currency)})
			if tt.name == "blank" {
				if !errors.Is(err, ErrSettingsCurrencyRequired) {
					t.Fatalf("expected ErrSettingsCurrencyRequired, got %v", err)
				}
				return
			}
			if !errors.Is(err, ErrSettingsCurrencyInvalid) {
				t.Fatalf("expected ErrSettingsCurrencyInvalid, got %v", err)
			}
		})
	}
}

func TestSettingsService_Update_InvalidPaymentTerms(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	_, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{PaymentTerms: intPtr(-1)})
	if !errors.Is(err, ErrSettingsPaymentTermsInvalid) {
		t.Fatalf("expected ErrSettingsPaymentTermsInvalid, got %v", err)
	}
}

// TestSettingsService_Update_PaymentTermsZeroIsValid proves 0 ("due
// immediately") is accepted, not treated as a blank/missing value.
func TestSettingsService_Update_PaymentTermsZeroIsValid(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	updated, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{PaymentTerms: intPtr(0)})
	if err != nil {
		t.Fatalf("expected payment terms 0 to be valid, got error: %v", err)
	}
	if updated.PaymentTerms != 0 {
		t.Errorf("expected payment terms 0, got %d", updated.PaymentTerms)
	}
}

func TestSettingsService_Update_BlankInvoicePrefixRejected(t *testing.T) {
	organisationID := uuid.New()
	service, _ := newTestSettingsService(t, organisationID)

	_, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{InvoicePrefix: strPtr("   ")})
	if !errors.Is(err, ErrSettingsInvoicePrefixRequired) {
		t.Fatalf("expected ErrSettingsInvoicePrefixRequired, got %v", err)
	}
}

// TestSettingsService_Update_NeverTouchesInvoiceNumber proves PATCH can
// never reset or overwrite the invoice-numbering counter, which belongs
// exclusively to the locked allocate-and-increment sequence in
// InvoiceService.Create.
func TestSettingsService_Update_NeverTouchesInvoiceNumber(t *testing.T) {
	organisationID := uuid.New()
	service, repository := newTestSettingsService(t, organisationID)

	settings := repository.settings[organisationID]
	settings.InvoiceNumber = 42
	repository.settings[organisationID] = settings

	if _, err := service.Update(context.Background(), organisationID, UpdateSettingsRequest{Currency: strPtr("EUR")}); err != nil {
		t.Fatalf("update: %v", err)
	}

	if repository.settings[organisationID].InvoiceNumber != 42 {
		t.Errorf("expected InvoiceNumber to remain 42, got %d", repository.settings[organisationID].InvoiceNumber)
	}
}

// TestSettingsService_Update_TenantIsolation proves updating one
// organisation's settings never affects another's.
func TestSettingsService_Update_TenantIsolation(t *testing.T) {
	orgA := uuid.New()
	orgB := uuid.New()

	repository := newFakeSettingsRepository()
	seedSettings(t, repository, orgA)
	seedSettings(t, repository, orgB)
	service := NewSettingsService(repository)

	if _, err := service.Update(context.Background(), orgA, UpdateSettingsRequest{Currency: strPtr("EUR")}); err != nil {
		t.Fatalf("update org A: %v", err)
	}

	settingsB, err := service.Get(context.Background(), orgB)
	if err != nil {
		t.Fatalf("get org B settings: %v", err)
	}
	if settingsB.Currency != "GBP" {
		t.Errorf("expected org B's currency to remain the default %q, got %q", "GBP", settingsB.Currency)
	}
}
