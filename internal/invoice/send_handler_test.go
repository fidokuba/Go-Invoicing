package invoice

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

func TestInvoiceHandler_Send_Success(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response InvoiceResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Status != InvoiceStatusSent {
		t.Errorf("expected status %q, got %q", InvoiceStatusSent, response.Status)
	}

	if response.SentAt == nil {
		t.Error("expected sentAt to be populated")
	}
}

func TestInvoiceHandler_Send_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodPost, "/invoices/not-a-uuid/send", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Send_MissingAuthenticatedContext(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Send_NotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	id := uuid.New()
	request := httptest.NewRequest(http.MethodPost, "/invoices/"+id.String()+"/send", nil)
	request.SetPathValue("id", id.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Send_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Send_AlreadySent(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusSent)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_Send_Paid(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusPaid)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_Send_MissingSellerNameReturnsConflict proves
// Milestone 7 Part 2's business-data-incompleteness mapping: an invoice
// that's a valid Draft but whose organisation has no resolvable seller
// name maps to 409, not 500 — "exists but can't currently be finalised",
// the same category as an already-Sent invoice.
func TestInvoiceHandler_Send_MissingSellerNameReturnsConflict(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: ""}

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_Send_MissingSettingsReturnsConflict mirrors the
// above for a missing organisation settings row — the identical
// "business data incomplete" category Create's own
// ErrInvoiceSettingsNotFound already represents, mapped to 409 here
// rather than Create's 500 since Send's own contract explicitly calls
// for it.
func TestInvoiceHandler_Send_MissingSettingsReturnsConflict(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	f.settingsRepository = newFakeSettingsRepository() // no settings row at all
	f.service.settingsRepository = f.settingsRepository

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_Send_OrganisationLookupFailureReturnsGenericServerError
// is a Milestone 7 hardening-pass regression test: a genuine failure
// loading the organisation (as opposed to a resolvable-but-blank field)
// must map to a generic 500 — never a 409 with the underlying error's
// text exposed. Deleting the organisation from the fake's map makes
// organisationRepository.GetByID return admin.ErrOrganisationNotFound,
// which InvoiceService.Send wraps as ErrInvoiceSnapshotDataUnavailable;
// this proves that sentinel is excluded from isSnapshotIncompleteError's
// 409 set and that no internal detail reaches the response body.
func TestInvoiceHandler_Send_OrganisationLookupFailureReturnsGenericServerError(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	delete(f.organisationRepository.organisations, f.organisationID)

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}

	body := recorder.Body.String()
	for _, leaked := range []string{"organisation not found", "ErrOrganisationNotFound", "look up organisation", "unavailable"} {
		if strings.Contains(body, leaked) {
			t.Errorf("expected the generic 500 body not to leak internal detail, but it contained %q: %s", leaked, body)
		}
	}
}

// TestInvoiceHandler_Send_IgnoresOrganisationIdQueryParameter proves a
// client cannot use ?organisationId=<other> to send another
// organisation's invoice — the authenticated context alone decides
// tenant scope, consistent with every other invoice/payment route.
func TestInvoiceHandler_Send_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(10000, InvoiceStatusDraft)
	attackerOrganisationID := uuid.New()

	request := httptest.NewRequest(http.MethodPost, "/invoices/"+invoiceID.String()+"/send?organisationId="+f.organisationID.String(), nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.Send(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}
