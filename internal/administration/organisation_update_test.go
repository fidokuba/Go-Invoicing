package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func strPtr(s string) *string { return &s }

// --- Service-level tests ---

func TestOrganisationService_Update_OmittedFieldsUnchanged(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	// First, set Email via an explicit update.
	updated, err := service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{
		Email: strPtr("hello@acme.test"),
	})
	if err != nil {
		t.Fatalf("update organisation: %v", err)
	}
	if updated.Email == nil || *updated.Email != "hello@acme.test" {
		t.Fatalf("expected email to be set, got %v", updated.Email)
	}

	// A second update that only touches Name must leave Email untouched.
	updated, err = service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{
		Name: strPtr("Acme Holdings Ltd"),
	})
	if err != nil {
		t.Fatalf("update organisation (name only): %v", err)
	}

	if updated.Name != "Acme Holdings Ltd" {
		t.Errorf("expected name to be updated, got %q", updated.Name)
	}

	if updated.Email == nil || *updated.Email != "hello@acme.test" {
		t.Errorf("expected email to remain unchanged at %q, got %v", "hello@acme.test", updated.Email)
	}
}

func TestOrganisationService_Update_SuppliedFieldsUpdate(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	updated, err := service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{
		Email:      strPtr("hello@acme.test"),
		Phone:      strPtr("+44 20 7946 0958"),
		Website:    strPtr("https://acme.test"),
		Address:    strPtr("1 Acme Way"),
		City:       strPtr("London"),
		State:      strPtr(""),
		PostalCode: strPtr("E1 6AN"),
		Country:    strPtr("GB"),
		TaxID:      strPtr("GB123456789"),
	})
	if err != nil {
		t.Fatalf("update organisation: %v", err)
	}

	checks := map[string]*string{
		"email":      updated.Email,
		"phone":      updated.Phone,
		"website":    updated.Website,
		"address":    updated.Address,
		"city":       updated.City,
		"postalCode": updated.PostalCode,
		"country":    updated.Country,
		"taxId":      updated.TaxID,
	}
	for field, got := range checks {
		if got == nil || *got == "" {
			t.Errorf("expected %s to be set, got %v", field, got)
		}
	}

	if updated.State != nil {
		t.Errorf("expected an explicitly empty State to clear the field (nil), got %v", *updated.State)
	}
}

func TestOrganisationService_Update_BlankNameRejected(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	_, err = service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{
		Name: strPtr("   "),
	})
	if !errors.Is(err, ErrOrganisationNameRequired) {
		t.Fatalf("expected ErrOrganisationNameRequired, got %v", err)
	}

	// The name must remain the original value — a rejected update must not
	// partially apply.
	current, err := repository.GetByID(context.Background(), organisation.ID)
	if err != nil {
		t.Fatalf("get organisation: %v", err)
	}
	if current.Name != "Acme Ltd" {
		t.Errorf("expected name to remain %q after a rejected update, got %q", "Acme Ltd", current.Name)
	}
}

func TestOrganisationService_Update_InvalidEmailRejected(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	_, err = service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{
		Email: strPtr("not-an-email"),
	})
	if !errors.Is(err, ErrOrganisationEmailInvalid) {
		t.Fatalf("expected ErrOrganisationEmailInvalid, got %v", err)
	}
}

func TestOrganisationService_Update_EmptyEmailClearsField(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	if _, err := service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{Email: strPtr("hello@acme.test")}); err != nil {
		t.Fatalf("set email: %v", err)
	}

	updated, err := service.Update(context.Background(), organisation.ID, UpdateOrganisationRequest{Email: strPtr("")})
	if err != nil {
		t.Fatalf("clear email: %v", err)
	}

	if updated.Email != nil {
		t.Errorf("expected an explicitly empty Email to clear the field, got %v", *updated.Email)
	}
}

// --- Handler-level tests: role/authentication behaviour is covered at
// the app level (TestApp_RoleMatrix_OrganisationUpdate); these exercise
// the handler's own request/response mapping directly. ---

func TestOrganisationHandler_Update_Success(t *testing.T) {
	repository := newFakeOrganisationRepository()
	service := NewOrganisationService(repository, newFakeSettingsRepository())
	handler := NewOrganisationHandler(service)

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{"email":"hello@acme.test","city":"London"}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response OrganisationResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Email == nil || *response.Email != "hello@acme.test" {
		t.Errorf("expected email in response, got %v", response.Email)
	}
	if response.City == nil || *response.City != "London" {
		t.Errorf("expected city in response, got %v", response.City)
	}
}

func TestOrganisationHandler_Update_BlankName(t *testing.T) {
	handler := newTestHandler()
	organisation, err := handler.service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{"name":""}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_Update_InvalidEmail(t *testing.T) {
	handler := newTestHandler()
	organisation, err := handler.service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{"email":"not-an-email"}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_Update_InvalidJSON(t *testing.T) {
	handler := newTestHandler()
	organisation, err := handler.service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_Update_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"email":"hello@acme.test"}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestOrganisationHandler_Update_NotFound proves Update returns 404 for an
// authenticated identity whose OrganisationID doesn't match any stored
// organisation — mirroring TestOrganisationHandler_GetCurrent_NotFound.
func TestOrganisationHandler_Update_NotFound(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"email":"hello@acme.test"}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
