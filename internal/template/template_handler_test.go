package template

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

// withAuthenticatedOrganisation mirrors internal/customer's own helper
// of the same name exactly — these tests invoke the handler directly,
// bypassing AuthMiddleware, so they must set up the same request
// context it would have.
func withAuthenticatedOrganisation(r *http.Request, organisationID uuid.UUID) *http.Request {
	identity := admin.AuthenticatedUser{UserID: uuid.New(), OrganisationID: organisationID, Role: admin.UserRoleUser}
	return r.WithContext(admin.WithAuthenticatedUser(r.Context(), identity))
}

func newTestTemplateHandler() (*TemplateHandler, *fakeTemplateRepository) {
	repository := newFakeTemplateRepository()
	tx := &fakeTx{}
	txBeginner := &fakeTxBeginner{tx: tx}
	service := NewTemplateService(repository, txBeginner)
	return NewTemplateHandler(service), repository
}

func TestTemplateHandler_Create_Success(t *testing.T) {
	handler, _ := newTestTemplateHandler()
	orgID := uuid.New()

	body := bytes.NewBufferString(`{"name":"My Layout","definition":{"content":[],"root":{}}}`)
	request := httptest.NewRequest(http.MethodPost, "/templates", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, orgID)
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusCreated, recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("ETag") != `"1"` {
		t.Errorf(`expected ETag "1", got %q`, recorder.Header().Get("ETag"))
	}

	var response TemplateResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if response.Name != "My Layout" {
		t.Errorf("expected name %q, got %q", "My Layout", response.Name)
	}
	if response.IsDefault || response.IsSystem {
		t.Errorf("expected a new template to be neither default nor system, got %+v", response)
	}
}

func TestTemplateHandler_Create_EmptyNameRejected(t *testing.T) {
	handler, _ := newTestTemplateHandler()

	body := bytes.NewBufferString(`{"name":"","definition":{}}`)
	request := httptest.NewRequest(http.MethodPost, "/templates", body)
	request.Header.Set("Content-Type", "application/json")
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.Create(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_List_ReturnsOrganisationsTemplates(t *testing.T) {
	handler, _ := newTestTemplateHandler()
	orgID := uuid.New()

	createBody := bytes.NewBufferString(`{"name":"My Layout","definition":{}}`)
	createRequest := httptest.NewRequest(http.MethodPost, "/templates", createBody)
	createRequest.Header.Set("Content-Type", "application/json")
	createRequest = withAuthenticatedOrganisation(createRequest, orgID)
	handler.Create(httptest.NewRecorder(), createRequest)

	request := httptest.NewRequest(http.MethodGet, "/templates", nil)
	request = withAuthenticatedOrganisation(request, orgID)
	recorder := httptest.NewRecorder()

	handler.List(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response TemplateListResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(response.Items) != 1 {
		t.Fatalf("expected 1 template, got %d", len(response.Items))
	}
}

func TestTemplateHandler_GetByID_NotFound(t *testing.T) {
	handler, _ := newTestTemplateHandler()

	request := httptest.NewRequest(http.MethodGet, "/templates/"+uuid.NewString(), nil)
	request.SetPathValue("id", uuid.NewString())
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.GetByID(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_Update_RequiresIfMatch(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: sampleDefinition}
	_ = repository.Create(context.Background(), created)

	body := bytes.NewBufferString(`{"name":"Renamed","definition":{}}`)
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+created.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("id", created.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	if recorder.Code != http.StatusPreconditionRequired {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusPreconditionRequired, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_Update_Success(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: sampleDefinition}
	_ = repository.Create(context.Background(), created)

	body := bytes.NewBufferString(`{"name":"Renamed","definition":{}}`)
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+created.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"1"`)
	req.SetPathValue("id", created.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}
	if recorder.Header().Get("ETag") != `"2"` {
		t.Errorf(`expected ETag "2", got %q`, recorder.Header().Get("ETag"))
	}
}

func TestTemplateHandler_Update_StaleVersionConflict(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: sampleDefinition}
	_ = repository.Create(context.Background(), created)

	body := bytes.NewBufferString(`{"name":"Renamed","definition":{}}`)
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+created.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"99"`)
	req.SetPathValue("id", created.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	if recorder.Code != http.StatusPreconditionFailed {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusPreconditionFailed, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_Update_SystemTemplateConflict(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	system := &Template{ID: uuid.New(), OrganisationID: orgID, Name: ClassicTemplateName, Definition: sampleDefinition, IsDefault: true, IsSystem: true}
	_ = repository.Create(context.Background(), system)

	body := bytes.NewBufferString(`{"name":"Renamed Classic","definition":{}}`)
	req := httptest.NewRequest(http.MethodPatch, "/templates/"+system.ID.String(), body)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("If-Match", `"1"`)
	req.SetPathValue("id", system.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_Delete_Success(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: sampleDefinition}
	_ = repository.Create(context.Background(), created)

	req := httptest.NewRequest(http.MethodDelete, "/templates/"+created.ID.String(), nil)
	req.SetPathValue("id", created.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Delete(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNoContent, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_Delete_SystemTemplateConflict(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	system := &Template{ID: uuid.New(), OrganisationID: orgID, Name: ClassicTemplateName, Definition: sampleDefinition, IsDefault: true, IsSystem: true}
	_ = repository.Create(context.Background(), system)

	req := httptest.NewRequest(http.MethodDelete, "/templates/"+system.ID.String(), nil)
	req.SetPathValue("id", system.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.Delete(recorder, req)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_SetDefault_Success(t *testing.T) {
	handler, repository := newTestTemplateHandler()
	orgID := uuid.New()

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: sampleDefinition}
	_ = repository.Create(context.Background(), created)

	req := httptest.NewRequest(http.MethodPost, "/templates/"+created.ID.String()+"/default", nil)
	req.SetPathValue("id", created.ID.String())
	req = withAuthenticatedOrganisation(req, orgID)
	recorder := httptest.NewRecorder()

	handler.SetDefault(recorder, req)

	if recorder.Code != http.StatusNoContent {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNoContent, recorder.Code, recorder.Body.String())
	}
}

func TestTemplateHandler_SetDefault_NotFound(t *testing.T) {
	handler, _ := newTestTemplateHandler()

	req := httptest.NewRequest(http.MethodPost, "/templates/"+uuid.NewString()+"/default", nil)
	req.SetPathValue("id", uuid.NewString())
	req = withAuthenticatedOrganisation(req, uuid.New())
	recorder := httptest.NewRecorder()

	handler.SetDefault(recorder, req)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
