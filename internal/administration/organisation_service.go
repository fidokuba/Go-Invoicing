package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// ErrOrganisationNameRequired is returned when Create is called with an
// empty (or whitespace-only) name. Handlers can use errors.Is to turn this
// into a 400 rather than a generic 500.
var ErrOrganisationNameRequired = errors.New("organisation name is required")

// OrganisationService sits between the HTTP layer and the repository. It
// depends on the OrganisationRepository interface, not on any concrete
// implementation, so it doesn't know or care that organisations happen to
// live in PostgreSQL today. It also depends on SettingsRepository, purely
// to provision a new organisation's settings row — see Create.
type OrganisationService struct {
	repository         OrganisationRepository
	settingsRepository SettingsRepository
}

func NewOrganisationService(
	repository OrganisationRepository,
	settingsRepository SettingsRepository,
) *OrganisationService {
	return &OrganisationService{
		repository:         repository,
		settingsRepository: settingsRepository,
	}
}

// Create validates the requested name, generates the organisation's ID,
// and persists it, then provisions a default settings row for it. The
// caller supplies only what a client is allowed to specify — the service,
// not the client, decides the ID.
//
// The settings row exists so invoice number allocation always has
// something to lock (see invoice.InvoiceService.Create) — every
// organisation is expected to have exactly one, per the settings table's
// own UNIQUE(organisation_id) constraint. InvoicePrefix/InvoiceNumber/
// Currency/PaymentTerms are supplied explicitly rather than left to the
// settings table's column defaults; InvoiceNumber starts at 0 ("nothing
// allocated yet") so the first invoice created for this organisation gets
// number 1 via Settings.NextInvoiceNumber.
//
// This is not wrapped in a transaction with the organisation INSERT: if
// settings creation fails after the organisation is created, the
// organisation is left without settings, and invoice creation for it will
// fail with ErrInvoiceSettingsNotFound until settings are added by hand.
// That gap is accepted for this milestone part rather than introducing a
// second transactional path alongside invoice creation's.
func (s *OrganisationService) Create(
	ctx context.Context,
	name string,
) (*Organisation, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrOrganisationNameRequired
	}

	organisation := &Organisation{
		ID:   uuid.New(),
		Name: name,
	}

	if err := s.repository.Create(ctx, organisation); err != nil {
		return nil, err
	}

	settings := &Settings{
		ID:             uuid.New(),
		OrganisationID: organisation.ID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}

	if err := s.settingsRepository.Create(ctx, settings); err != nil {
		return nil, fmt.Errorf("create organisation settings: %w", err)
	}

	return organisation, nil
}

func (s *OrganisationService) GetByID(
	ctx context.Context,
	id uuid.UUID,
) (*Organisation, error) {
	return s.repository.GetByID(ctx, id)
}
