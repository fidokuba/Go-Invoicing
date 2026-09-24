package admin

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
)

// ErrOrganisationNameRequired is returned when Create is called with an
// empty (or whitespace-only) name. Handlers can use errors.Is to turn this
// into a 400 rather than a generic 500. Update (Milestone 7 Part 1)
// returns the same error if explicitly asked to set Name to blank — Name
// may never become empty, whether at creation or update.
var ErrOrganisationNameRequired = errors.New("organisation name is required")

// ErrOrganisationEmailInvalid is returned by Update when given a
// non-blank Email that fails basic RFC 5322 syntax validation (via
// net/mail.ParseAddress — the standard library's own check, no bespoke
// regex). A blank Email is not an error: see Update's own comment for why
// an explicitly-supplied empty optional field clears it instead.
var ErrOrganisationEmailInvalid = errors.New("organisation email is not a valid email address")

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

// Update applies a partial update to organisationID's own organisation —
// there is no other organisation this can ever target; the caller
// (OrganisationHandler.Update) always passes AuthenticatedUser
// .OrganisationID, never a client-supplied ID.
//
// Genuine PATCH semantics: a nil field in request is left completely
// unchanged; a non-nil field is applied. For every field except Name, an
// explicitly-supplied empty string clears the field (stored as NULL) —
// these are all optional business-detail fields, and "the caller sent an
// empty value on purpose" is a meaningful, different request from
// "the caller didn't mention this field at all". Name is the one
// exception: it is NOT NULL and business-required, so an explicit empty
// Name is rejected with ErrOrganisationNameRequired rather than being
// allowed to clear the organisation's own name.
//
// Email additionally receives basic syntax validation via net/mail
// .ParseAddress when non-blank — Phone/Website/Address/City/State/
// PostalCode/Country/TaxID deliberately do not get equivalent format
// validation (Milestone 7 Part 1 explicitly avoids over-strict validation
// for those), only whitespace trimming.
//
// This reads the current organisation, applies only the supplied fields
// onto it, and writes the whole merged value back via one repository
// Update call — no dynamic/reflection-based patch SQL, matching this
// project's existing "smallest clear implementation" convention (e.g.
// InvoiceService.Send's explicit domain method over a generic state
// machine).
//
// Optimistic concurrency (Milestone 13 Part 2): expectedVersion is the
// version the caller last read (its If-Match). A mismatch against the
// version just read fails fast with ErrOrganisationVersionConflict, but
// the authoritative check is the repository's atomic
// "WHERE version = expectedVersion" — which also catches a concurrent
// write landing between this read and the write below, so a merged
// value built from a stale read can never overwrite a newer one.
func (s *OrganisationService) Update(
	ctx context.Context,
	organisationID uuid.UUID,
	expectedVersion int64,
	request UpdateOrganisationRequest,
) (*Organisation, error) {
	organisation, err := s.repository.GetByID(ctx, organisationID)
	if err != nil {
		return nil, err
	}

	if organisation.Version != expectedVersion {
		return nil, ErrOrganisationVersionConflict
	}

	if request.Name != nil {
		name := strings.TrimSpace(*request.Name)
		if name == "" {
			return nil, ErrOrganisationNameRequired
		}
		organisation.Name = name
	}

	if request.Email != nil {
		email := strings.TrimSpace(*request.Email)
		if email != "" {
			if _, err := mail.ParseAddress(email); err != nil {
				return nil, ErrOrganisationEmailInvalid
			}
		}
		organisation.Email = nilIfEmpty(email)
	}

	if request.Phone != nil {
		organisation.Phone = nilIfEmpty(*request.Phone)
	}

	if request.Website != nil {
		organisation.Website = nilIfEmpty(*request.Website)
	}

	if request.Address != nil {
		organisation.Address = nilIfEmpty(*request.Address)
	}

	if request.City != nil {
		organisation.City = nilIfEmpty(*request.City)
	}

	if request.State != nil {
		organisation.State = nilIfEmpty(*request.State)
	}

	if request.PostalCode != nil {
		organisation.PostalCode = nilIfEmpty(*request.PostalCode)
	}

	if request.Country != nil {
		organisation.Country = nilIfEmpty(*request.Country)
	}

	if request.TaxID != nil {
		organisation.TaxID = nilIfEmpty(*request.TaxID)
	}

	if err := s.repository.Update(ctx, organisationID, organisation, expectedVersion); err != nil {
		return nil, err
	}

	return organisation, nil
}

// nilIfEmpty converts a blank/whitespace-only string into a nil pointer so
// an optional field is stored as SQL NULL rather than an empty string —
// already-trimmed values are passed in as-is (nilIfEmpty trims again
// defensively, which is harmless).
func nilIfEmpty(value string) *string {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	return &value
}
