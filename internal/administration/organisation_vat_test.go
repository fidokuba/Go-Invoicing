package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func boolPtr(b bool) *bool { return &b }

func TestOrganisationService_Create_NotVATRegisteredByDefault(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), newFakeSettingsRepository())

	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	if organisation.VATRegistered {
		t.Error("expected a new organisation not to be VAT registered")
	}
}

func TestOrganisationService_Update_VATRegisteredWithTaxID(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), newFakeSettingsRepository())
	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	updated, err := service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
		VATRegistered: boolPtr(true),
		TaxID:         strPtr("GB123456789"),
	})
	if err != nil {
		t.Fatalf("update organisation: %v", err)
	}

	if !updated.VATRegistered {
		t.Error("expected the organisation to be VAT registered")
	}
}

func TestOrganisationService_Update_VATRegisteredRequiresTaxID(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), newFakeSettingsRepository())
	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	for name, taxID := range map[string]*string{"omitted": nil, "blank": strPtr("   ")} {
		t.Run(name, func(t *testing.T) {
			_, err := service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
				VATRegistered: boolPtr(true),
				TaxID:         taxID,
			})
			if !errors.Is(err, ErrOrganisationVATNumberRequired) {
				t.Fatalf("expected ErrOrganisationVATNumberRequired, got %v", err)
			}
		})
	}
}

// TestOrganisationService_Update_ClearingTaxIDWhileRegisteredRejected:
// the rule applies to the merged result, not only to requests that touch
// the VAT flag.
func TestOrganisationService_Update_ClearingTaxIDWhileRegisteredRejected(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), newFakeSettingsRepository())
	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	if _, err := service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
		VATRegistered: boolPtr(true),
		TaxID:         strPtr("GB123456789"),
	}); err != nil {
		t.Fatalf("register for VAT: %v", err)
	}

	_, err = service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
		TaxID: strPtr(""),
	})
	if !errors.Is(err, ErrOrganisationVATNumberRequired) {
		t.Fatalf("expected ErrOrganisationVATNumberRequired, got %v", err)
	}
}

// TestOrganisationService_Update_DeregisteringKeepsTaxID: unticking VAT
// registration keeps the stored TaxID so re-registering restores it.
func TestOrganisationService_Update_DeregisteringKeepsTaxID(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), newFakeSettingsRepository())
	organisation, err := service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	if _, err := service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
		VATRegistered: boolPtr(true),
		TaxID:         strPtr("GB123456789"),
	}); err != nil {
		t.Fatalf("register for VAT: %v", err)
	}

	updated, err := service.Update(context.Background(), organisation.ID, organisationVersion(t, service, organisation.ID), UpdateOrganisationRequest{
		VATRegistered: boolPtr(false),
	})
	if err != nil {
		t.Fatalf("deregister for VAT: %v", err)
	}

	if updated.VATRegistered {
		t.Error("expected the organisation not to be VAT registered")
	}

	if updated.TaxID == nil || *updated.TaxID != "GB123456789" {
		t.Errorf("expected the stored Tax ID to be kept, got %v", updated.TaxID)
	}
}

func TestOrganisationHandler_Update_VATRegisteredWithoutTaxID(t *testing.T) {
	handler := newTestHandler()
	organisation, err := handler.service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{"vatRegistered":true}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`) // a newly created organisation's version
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestOrganisationHandler_Update_VATRegisteredInResponse(t *testing.T) {
	handler := newTestHandler()
	organisation, err := handler.service.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}

	body := bytes.NewBufferString(`{"vatRegistered":true,"taxId":"GB123456789"}`)
	request := httptest.NewRequest(http.MethodPatch, "/organisation", body)
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("If-Match", `"1"`) // a newly created organisation's version
	request = withAuthenticatedOrganisation(request, organisation.ID)
	recorder := httptest.NewRecorder()

	handler.Update(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response map[string]any
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response["vatRegistered"] != true {
		t.Errorf("expected vatRegistered=true in the response, got %v", response["vatRegistered"])
	}
	if response["taxId"] != "GB123456789" {
		t.Errorf("expected taxId in the response, got %v", response["taxId"])
	}
}
