package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeOrganisationRepository is an in-memory OrganisationRepository used to
// test the handler/service without touching PostgreSQL. The repository
// integration is already covered by organisation_repository_postgres_test.go.
type fakeOrganisationRepository struct {
	mu            sync.Mutex
	organisations map[uuid.UUID]Organisation

	createErr error
}

func newFakeOrganisationRepository() *fakeOrganisationRepository {
	return &fakeOrganisationRepository{
		organisations: make(map[uuid.UUID]Organisation),
	}
}

// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional semantics of its own, matching
// fakeSettingsRepository's own WithTx below.
func (f *fakeOrganisationRepository) WithTx(tx pgx.Tx) OrganisationRepository {
	return f
}

func (f *fakeOrganisationRepository) Create(ctx context.Context, organisation *Organisation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return f.createErr
	}

	// Mirrors the column default: a new organisation starts at version 1.
	organisation.Version = 1
	f.organisations[organisation.ID] = *organisation
	return nil
}

func (f *fakeOrganisationRepository) GetByID(ctx context.Context, id uuid.UUID) (*Organisation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	organisation, ok := f.organisations[id]
	if !ok {
		return nil, ErrOrganisationNotFound
	}

	return &organisation, nil
}

// Update mirrors PostgresOrganisationRepository.Update's optimistic
// concurrency contract: the write only happens if the stored version
// equals expectedVersion, and then increments it.
func (f *fakeOrganisationRepository) Update(ctx context.Context, organisationID uuid.UUID, organisation *Organisation, expectedVersion int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	current, ok := f.organisations[organisationID]
	if !ok {
		return ErrOrganisationNotFound
	}
	if current.Version != expectedVersion {
		return ErrOrganisationVersionConflict
	}

	organisation.Version = expectedVersion + 1
	f.organisations[organisationID] = *organisation
	return nil
}

// fakeSettingsRepository is an in-memory SettingsRepository used to test
// OrganisationService's settings provisioning without touching
// PostgreSQL. WithTx ignores its tx argument and returns the same fake —
// it has no real transactional semantics of its own.
type fakeSettingsRepository struct {
	mu       sync.Mutex
	settings map[uuid.UUID]Settings

	createErr error
}

func newFakeSettingsRepository() *fakeSettingsRepository {
	return &fakeSettingsRepository{
		settings: make(map[uuid.UUID]Settings),
	}
}

func (f *fakeSettingsRepository) WithTx(tx pgx.Tx) SettingsRepository {
	return f
}

func (f *fakeSettingsRepository) Create(ctx context.Context, settings *Settings) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return f.createErr
	}

	// Mirrors the column default: new settings start at version 1.
	settings.Version = 1
	f.settings[settings.OrganisationID] = *settings
	return nil
}

func (f *fakeSettingsRepository) GetByOrganisationID(ctx context.Context, organisationID uuid.UUID) (*Settings, error) {
	return f.get(organisationID)
}

func (f *fakeSettingsRepository) GetForUpdate(ctx context.Context, organisationID uuid.UUID) (*Settings, error) {
	return f.get(organisationID)
}

func (f *fakeSettingsRepository) get(organisationID uuid.UUID) (*Settings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.settings[organisationID]
	if !ok {
		return nil, ErrSettingsNotFound
	}
	return &s, nil
}

func (f *fakeSettingsRepository) UpdateInvoiceNumber(ctx context.Context, organisationID uuid.UUID, invoiceNumber int) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.settings[organisationID]
	if !ok {
		return ErrSettingsNotFound
	}
	s.InvoiceNumber = invoiceNumber
	f.settings[organisationID] = s
	return nil
}

// Update persists InvoicePrefix/Currency/PaymentTerms only — mirroring
// PostgresSettingsRepository.Update's own contract, including never
// touching InvoiceNumber.
func (f *fakeSettingsRepository) Update(ctx context.Context, organisationID uuid.UUID, settings *Settings, expectedVersion int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	s, ok := f.settings[organisationID]
	if !ok {
		return ErrSettingsNotFound
	}
	if s.Version != expectedVersion {
		return ErrSettingsVersionConflict
	}

	s.Version = expectedVersion + 1
	settings.Version = s.Version

	s.InvoicePrefix = settings.InvoicePrefix
	s.Currency = settings.Currency
	s.PaymentTerms = settings.PaymentTerms
	f.settings[organisationID] = s

	settings.UpdatedAt = s.UpdatedAt
	return nil
}

func newTestHandler() *OrganisationHandler {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())
	return NewOrganisationHandler(service)
}

// withAuthenticatedOrganisation attaches an AuthenticatedUser identity
// scoped to organisationID to r, the way AuthMiddleware.RequireAuth would
// have — for tests invoking a protected handler directly, bypassing the
// middleware.
func withAuthenticatedOrganisation(r *http.Request, organisationID uuid.UUID) *http.Request {
	identity := AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: UserRoleUser}
	return r.WithContext(WithAuthenticatedUser(r.Context(), identity))
}

// withAuthenticatedIdentity attaches identity to r's context directly,
// for tests that need to control UserID and/or Role as well as
// OrganisationID (e.g. self-vs-other and role-based authorisation tests),
// not just the organisation withAuthenticatedOrganisation covers.
func withAuthenticatedIdentity(r *http.Request, identity AuthenticatedUser) *http.Request {
	return r.WithContext(WithAuthenticatedUser(r.Context(), identity))
}

func TestOrganisationHandler_GetCurrent(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())
	handler := NewOrganisationHandler(service)

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/organisation", nil)
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.GetCurrent(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response OrganisationResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.ID != organisation.ID.String() {
		t.Errorf("expected ID %q, got %q", organisation.ID.String(), response.ID)
	}
}

// TestOrganisationHandler_GetCurrent_NotFound proves that GetCurrent
// still returns 404 for an authenticated identity whose OrganisationID
// doesn't match any stored organisation — there's no client-supplied ID
// to be invalid, but the organisation itself can still be gone (e.g. a
// stale session outliving its organisation, however unlikely today).
func TestOrganisationHandler_GetCurrent_NotFound(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/organisation", nil)
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.GetCurrent(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestOrganisationHandler_GetCurrent_MissingAuthenticatedContext proves
// GetCurrent fails closed (401) if invoked without going through
// AuthMiddleware.RequireAuth, rather than the 400 "invalid organisation
// ID" this handler used to return before Milestone 4 Part 4 — there is no
// client-supplied organisation ID left to be invalid or missing; the only
// remaining failure mode is a missing authenticated identity.
func TestOrganisationHandler_GetCurrent_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/organisation", nil)
	recorder := httptest.NewRecorder()

	handler.GetCurrent(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}
