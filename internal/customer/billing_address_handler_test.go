package customer

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

// createFakeCustomer creates a customer via customerRepository and
// registers the customer/organisation relationship with addressRepository
// — the two fakes are independent, so both need to know about the
// customer for a billing-address test to exercise tenant scoping the way
// the real, JOIN-based repository would.
func createFakeCustomer(t *testing.T, customerRepository *fakeCustomerRepository, addressRepository *fakeAddressRepository, organisationID uuid.UUID) uuid.UUID {
	t.Helper()

	c := &Customer{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Acme Ltd",
		Status:         "active",
	}

	if err := customerRepository.Create(context.Background(), c); err != nil {
		t.Fatalf("create fake customer: %v", err)
	}

	addressRepository.registerCustomer(c.ID, organisationID)

	return c.ID
}

func TestCustomerHandler_UpsertBillingAddress_CreatesFirst(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	organisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, organisationID)

	body := bytes.NewBufferString(`{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
	request := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", body)
	request.SetPathValue("id", customerID.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.UpsertBillingAddress(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response AddressResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Street != "1 Acme Way" || response.City != "London" || response.PostalCode != "E1 6AN" || response.Country != "GB" {
		t.Errorf("unexpected address fields in response: %+v", response)
	}

	if response.Type != AddressTypeBilling {
		t.Errorf("expected type %q, got %q", AddressTypeBilling, response.Type)
	}
}

// TestCustomerHandler_UpsertBillingAddress_SecondCallUpdatesNotDuplicates
// proves PUT is a genuine upsert: a second PUT with different fields
// replaces the same row (same ID) rather than creating a second one.
func TestCustomerHandler_UpsertBillingAddress_SecondCallUpdatesNotDuplicates(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	organisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, organisationID)

	firstBody := bytes.NewBufferString(`{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
	firstRequest := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", firstBody)
	firstRequest.SetPathValue("id", customerID.String())
	firstRequest = withAuthenticatedOrganisation(firstRequest, organisationID)
	firstRecorder := httptest.NewRecorder()
	handler.UpsertBillingAddress(firstRecorder, firstRequest)

	var first AddressResponse
	if err := json.NewDecoder(firstRecorder.Body).Decode(&first); err != nil {
		t.Fatalf("decode first response: %v", err)
	}

	secondBody := bytes.NewBufferString(`{"street":"2 New Street","city":"Manchester","postalCode":"M1 1AE","country":"GB"}`)
	secondRequest := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", secondBody)
	secondRequest.SetPathValue("id", customerID.String())
	secondRequest = withAuthenticatedOrganisation(secondRequest, organisationID)
	secondRecorder := httptest.NewRecorder()
	handler.UpsertBillingAddress(secondRecorder, secondRequest)

	if secondRecorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, secondRecorder.Code, secondRecorder.Body.String())
	}

	var second AddressResponse
	if err := json.NewDecoder(secondRecorder.Body).Decode(&second); err != nil {
		t.Fatalf("decode second response: %v", err)
	}

	if second.ID != first.ID {
		t.Errorf("expected the same address ID across upserts, got %q then %q", first.ID, second.ID)
	}

	if second.Street != "2 New Street" || second.City != "Manchester" {
		t.Errorf("expected the second PUT's fields to take effect, got %+v", second)
	}

	getRequest := httptest.NewRequest(http.MethodGet, "/customers/"+customerID.String()+"/billing-address", nil)
	getRequest.SetPathValue("id", customerID.String())
	getRequest = withAuthenticatedOrganisation(getRequest, organisationID)
	getRecorder := httptest.NewRecorder()
	handler.GetBillingAddress(getRecorder, getRequest)

	var fetched AddressResponse
	if err := json.NewDecoder(getRecorder.Body).Decode(&fetched); err != nil {
		t.Fatalf("decode GET response: %v", err)
	}

	if fetched.Street != "2 New Street" {
		t.Errorf("expected GET to reflect the latest upsert, got street %q", fetched.Street)
	}
}

func TestCustomerHandler_GetBillingAddress_NoneYet(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	organisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, organisationID)

	request := httptest.NewRequest(http.MethodGet, "/customers/"+customerID.String()+"/billing-address", nil)
	request.SetPathValue("id", customerID.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.GetBillingAddress(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetBillingAddress_InvalidUUID(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/customers/not-a-uuid/billing-address", nil)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.GetBillingAddress(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_UpsertBillingAddress_InvalidUUID(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
	request := httptest.NewRequest(http.MethodPut, "/customers/not-a-uuid/billing-address", body)
	request.SetPathValue("id", "not-a-uuid")
	request = withAuthenticatedOrganisation(request, uuid.New())
	recorder := httptest.NewRecorder()

	handler.UpsertBillingAddress(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_GetBillingAddress_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	request := httptest.NewRequest(http.MethodGet, "/customers/"+uuid.New().String()+"/billing-address", nil)
	request.SetPathValue("id", uuid.New().String())
	recorder := httptest.NewRecorder()

	handler.GetBillingAddress(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

func TestCustomerHandler_UpsertBillingAddress_MissingAuthenticatedContext(t *testing.T) {
	handler := newTestHandler()

	body := bytes.NewBufferString(`{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
	request := httptest.NewRequest(http.MethodPut, "/customers/"+uuid.New().String()+"/billing-address", body)
	request.SetPathValue("id", uuid.New().String())
	recorder := httptest.NewRecorder()

	handler.UpsertBillingAddress(recorder, request)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
	}
}

// TestCustomerHandler_GetBillingAddress_CrossTenant proves a foreign
// organisation's customer never reveals whether it exists or has an
// address — both collapse into the same 404.
func TestCustomerHandler_GetBillingAddress_CrossTenant(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	ownerOrganisationID := uuid.New()
	attackerOrganisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, ownerOrganisationID)

	upsertBody := bytes.NewBufferString(`{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN","country":"GB"}`)
	upsertRequest := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", upsertBody)
	upsertRequest.SetPathValue("id", customerID.String())
	upsertRequest = withAuthenticatedOrganisation(upsertRequest, ownerOrganisationID)
	handler.UpsertBillingAddress(httptest.NewRecorder(), upsertRequest)

	request := httptest.NewRequest(http.MethodGet, "/customers/"+customerID.String()+"/billing-address?organisationId="+ownerOrganisationID.String(), nil)
	request.SetPathValue("id", customerID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.GetBillingAddress(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}
}

// TestCustomerHandler_UpsertBillingAddress_CrossTenant proves a foreign
// organisation cannot create or overwrite another organisation's
// customer's billing address, even naming the real owner via
// ?organisationId=.
func TestCustomerHandler_UpsertBillingAddress_CrossTenant(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	ownerOrganisationID := uuid.New()
	attackerOrganisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, ownerOrganisationID)

	body := bytes.NewBufferString(`{"street":"Attacker Street","city":"Nowhere","postalCode":"00000","country":"XX"}`)
	request := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address?organisationId="+ownerOrganisationID.String(), body)
	request.SetPathValue("id", customerID.String())
	request = withAuthenticatedOrganisation(request, attackerOrganisationID)
	recorder := httptest.NewRecorder()

	handler.UpsertBillingAddress(recorder, request)

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("expected status %d (organisationId query parameter must be ignored), got %d (body: %s)", http.StatusNotFound, recorder.Code, recorder.Body.String())
	}

	// Confirm nothing was actually written for the real owner either.
	getRequest := httptest.NewRequest(http.MethodGet, "/customers/"+customerID.String()+"/billing-address", nil)
	getRequest.SetPathValue("id", customerID.String())
	getRequest = withAuthenticatedOrganisation(getRequest, ownerOrganisationID)
	getRecorder := httptest.NewRecorder()
	handler.GetBillingAddress(getRecorder, getRequest)

	if getRecorder.Code != http.StatusNotFound {
		t.Fatalf("expected the owner to still have no billing address after the blocked cross-tenant attempt, got status %d", getRecorder.Code)
	}
}

func TestCustomerHandler_UpsertBillingAddress_ValidationErrors(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{"missing street", `{"city":"London","postalCode":"E1 6AN","country":"GB"}`},
		{"missing city", `{"street":"1 Acme Way","postalCode":"E1 6AN","country":"GB"}`},
		{"missing postal code", `{"street":"1 Acme Way","city":"London","country":"GB"}`},
		{"missing country", `{"street":"1 Acme Way","city":"London","postalCode":"E1 6AN"}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, customerRepository, addressRepository := newTestHandlerWithFakes()
			organisationID := uuid.New()
			customerID := createFakeCustomer(t, customerRepository, addressRepository, organisationID)

			request := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", bytes.NewBufferString(tt.body))
			request.SetPathValue("id", customerID.String())
			request = withAuthenticatedOrganisation(request, organisationID)
			recorder := httptest.NewRecorder()

			handler.UpsertBillingAddress(recorder, request)

			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
			}
		})
	}
}

func TestCustomerHandler_UpsertBillingAddress_InvalidJSON(t *testing.T) {
	handler, customerRepository, addressRepository := newTestHandlerWithFakes()
	organisationID := uuid.New()
	customerID := createFakeCustomer(t, customerRepository, addressRepository, organisationID)

	request := httptest.NewRequest(http.MethodPut, "/customers/"+customerID.String()+"/billing-address", bytes.NewBufferString(`{`))
	request.SetPathValue("id", customerID.String())
	request = withAuthenticatedOrganisation(request, organisationID)
	recorder := httptest.NewRecorder()

	handler.UpsertBillingAddress(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}
