package invoice

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
)

func TestInvoiceHandler_GetPDF_Success(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.repository.lines[invoiceID] = []*Line{
		{ID: uuid.New(), InvoiceID: invoiceID, Description: "Consulting", Quantity: 1, UnitPrice: 1000, VATRate: 0, VATAmount: 0, Total: 1000},
	}

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/pdf" {
		t.Errorf("expected Content-Type application/pdf, got %q", contentType)
	}

	disposition := recorder.Header().Get("Content-Disposition")
	if !strings.HasPrefix(disposition, "attachment; filename=\"invoice-") || !strings.HasSuffix(disposition, ".pdf\"") {
		t.Errorf("expected an attachment Content-Disposition with an invoice-*.pdf filename, got %q", disposition)
	}

	if !strings.HasPrefix(recorder.Body.String(), "%PDF-") {
		t.Errorf("expected body to start with %%PDF-, got %q", recorder.Body.String()[:min(20, recorder.Body.Len())])
	}

	if recorder.Header().Get("Content-Length") == "" {
		t.Error("expected Content-Length to be set")
	}
}

func TestInvoiceHandler_GetPDF_FilenameUsesInvoiceNumber(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	inv := f.repository.invoices[invoiceID]
	inv.InvoiceNumber = "INV-777"
	f.repository.invoices[invoiceID] = inv

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	want := `attachment; filename="invoice-INV-777.pdf"`
	if got := recorder.Header().Get("Content-Disposition"); got != want {
		t.Errorf("expected Content-Disposition %q, got %q", want, got)
	}
}

func TestInvoiceHandler_GetPDF_InvalidUUID(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices/not-a-uuid/pdf", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPDF_MissingAuthenticatedContext(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf", nil)
	request.SetPathValue("id", invoiceID.String())
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPDF_NotFound(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+uuid.New().String()+"/pdf", nil)
	request.SetPathValue("id", uuid.New().String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPDF_WrongOrganisation(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestInvoiceHandler_GetPDF_IgnoresOrganisationIdQueryParameter proves a
// client cannot use ?organisationId=<other> to reach into another
// organisation's invoice PDF.
func TestInvoiceHandler_GetPDF_IgnoresOrganisationIdQueryParameter(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	attackerOrganisationID := uuid.New()

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf?organisationId="+f.organisationID.String(), nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestInvoiceHandler_GetPDF_SnapshotIncompleteReturnsConflict(t *testing.T) {
	f := newTestFixture()
	handler := newTestHandler(f)
	invoiceID := f.addInvoice(1000, InvoiceStatusDraft)
	f.organisationRepository.organisations[f.organisationID] = admin.Organisation{ID: f.organisationID, Name: ""}

	request := httptest.NewRequest(http.MethodGet, "/invoices/"+invoiceID.String()+"/pdf", nil)
	request.SetPathValue("id", invoiceID.String())
	request = withAuthenticatedOrganisation(request, f.organisationID)
	recorder := httptest.NewRecorder()

	handler.GetPDF(recorder, request)

	if recorder.Code != http.StatusConflict {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusConflict, recorder.Code, recorder.Body.String())
	}
}
