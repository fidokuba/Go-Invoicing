package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/google/uuid"
)

// fakeOrganisationRepository is an in-memory OrganisationRepository used to
// test the handler/service without touching PostgreSQL. The repository
// integration is already covered by organisation_repository_postgres_test.go.
type fakeOrganisationRepository struct {
	mu            sync.Mutex
	organisations map[uuid.UUID]Organisation
}

func newFakeOrganisationRepository() *fakeOrganisationRepository {
	return &fakeOrganisationRepository{
		organisations: make(map[uuid.UUID]Organisation),
	}
}

func (f *fakeOrganisationRepository) Create(ctx context.Context, organisation *Organisation) error {
	f.mu.Lock()
	defer f.mu.Unlock()

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

func newTestHandler() *OrganisationHandler {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository)
	return NewOrganisationHandler(service)
}

func TestOrganisationHandler_Create(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"Acme Ltd"}`)
	request := httptest.NewRequest(http.MethodPost, "/organisations", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}

	var response OrganisationResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Name != "Acme Ltd" {
		t.Errorf("expected name %q, got %q", "Acme Ltd", response.Name)
	}

	if _, err := uuid.Parse(response.ID); err != nil {
		t.Errorf("expected response ID to be a valid UUID, got %q", response.ID)
	}

	if response.CreatedAt == "" {
		t.Error("expected CreatedAt to be set")
	}
}

func TestOrganisationHandler_Create_MissingName(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"name":"   "}`)
	request := httptest.NewRequest(http.MethodPost, "/organisations", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_Create_InvalidJSON(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/organisations", body)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_GetByID(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository)
	handler := NewOrganisationHandler(service)

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	request := httptest.NewRequest(http.MethodGet, "/organisations/"+organisation.ID.String(), nil)
	request.SetPathValue("id", organisation.ID.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

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

func TestOrganisationHandler_GetByID_NotFound(t *testing.T) {
	handler := newTestHandler()

	id := uuid.New()
	request := httptest.NewRequest(http.MethodGet, "/organisations/"+id.String(), nil)
	request.SetPathValue("id", id.String())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_GetByID_InvalidUUID(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/organisations/not-a-uuid", nil)
	request.SetPathValue("id", "not-a-uuid")
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}
