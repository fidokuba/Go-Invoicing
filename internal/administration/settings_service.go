package admin

import (
	"context"
	"errors"
	"strings"

	"github.com/google/uuid"
)

// Settings validation sentinels (Milestone 8 Part 3). Currency reuses the
// exact same "trim, uppercase, exactly 3 ASCII letters" rule the invoice
// package's own snapshot/PDF currency validation already applies
// (isValidCurrencyCode there) — duplicated here as a small, local,
// self-contained check rather than importing the invoice package, which
// would create an import cycle (invoice already imports admin).
var (
	ErrSettingsCurrencyRequired      = errors.New("settings currency is required")
	ErrSettingsCurrencyInvalid       = errors.New("settings currency must be a 3-letter ISO-style code")
	ErrSettingsPaymentTermsInvalid   = errors.New("settings payment terms must not be negative")
	ErrSettingsInvoicePrefixRequired = errors.New("settings invoice prefix is required")
)

// SettingsService sits between the HTTP layer and the settings
// repository for the small, explicit set of fields GET/PATCH
// /organisation/settings exposes. It is deliberately a separate type
// from OrganisationService: organisation identity/address fields and
// invoice-numbering settings are different concerns with different
// validation rules, and Settings already has its own repository — adding
// a second service around it is simpler than growing OrganisationService
// to reach into a repository it doesn't otherwise touch.
type SettingsService struct {
	repository SettingsRepository
}

func NewSettingsService(repository SettingsRepository) *SettingsService {
	return &SettingsService{repository: repository}
}

// Get delegates straight to the repository; organisation scoping happens
// there.
func (s *SettingsService) Get(ctx context.Context, organisationID uuid.UUID) (*Settings, error) {
	return s.repository.GetByOrganisationID(ctx, organisationID)
}

// Update applies a partial update to organisationID's own settings —
// there is no other organisation this can ever target; the caller
// (SettingsHandler.Update) always passes AuthenticatedUser
// .OrganisationID, never a client-supplied ID.
//
// Genuine PATCH semantics, matching OrganisationService.Update: a nil
// field in request is left completely unchanged; a non-nil field is
// validated and applied. Unlike OrganisationService.Update, none of
// these three fields may be cleared by an explicit empty value — all
// three are business-required (or, for PaymentTerms, have 0 as their own
// meaningful "empty" value), so an explicitly-supplied blank/invalid
// value is rejected rather than treated as "clear this field".
//
// This reads the current settings, applies only the supplied fields onto
// it, and writes the whole merged value back via one repository Update
// call — no dynamic/reflection-based patch SQL, matching this project's
// "smallest clear implementation" convention.
func (s *SettingsService) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	request UpdateSettingsRequest,
) (*Settings, error) {
	settings, err := s.repository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		return nil, err
	}

	if request.Currency != nil {
		currency := strings.ToUpper(strings.TrimSpace(*request.Currency))
		if currency == "" {
			return nil, ErrSettingsCurrencyRequired
		}
		if !isValidSettingsCurrencyCode(currency) {
			return nil, ErrSettingsCurrencyInvalid
		}
		settings.Currency = currency
	}

	if request.PaymentTerms != nil {
		if *request.PaymentTerms < 0 {
			return nil, ErrSettingsPaymentTermsInvalid
		}
		settings.PaymentTerms = *request.PaymentTerms
	}

	if request.InvoicePrefix != nil {
		prefix := strings.TrimSpace(*request.InvoicePrefix)
		if prefix == "" {
			return nil, ErrSettingsInvoicePrefixRequired
		}
		settings.InvoicePrefix = prefix
	}

	if err := s.repository.Update(ctx, organisationID, settings); err != nil {
		return nil, err
	}

	return settings, nil
}

// isValidSettingsCurrencyCode reports whether code is exactly 3 ASCII
// uppercase letters — deliberately no exchange-rate or ISO-4217
// catalogue validation (Milestone 8 Part 3 explicitly excludes both):
// this only confirms the value is shaped like a currency code, not that
// it's a currency that actually exists.
func isValidSettingsCurrencyCode(code string) bool {
	if len(code) != 3 {
		return false
	}

	for _, r := range code {
		if r < 'A' || r > 'Z' {
			return false
		}
	}

	return true
}
