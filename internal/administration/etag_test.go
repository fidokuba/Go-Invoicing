package admin

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"go-invoicing/internal/httpx"
)

// Milestone 13 Part 2: optimistic concurrency on PATCH /organisation and
// PATCH /organisation/settings, at the handler level (fakes). The atomic
// SQL guarantee is proven against PostgreSQL in
// optimistic_concurrency_postgres_test.go.

func TestParseVersionETag(t *testing.T) {
	valid := map[string]int64{`"1"`: 1, `"42"`: 42, ` "7" `: 7, `"9223372036854775807"`: 9223372036854775807}
	for tag, want := range valid {
		if got, ok := parseVersionETag([]string{tag}); !ok || got != want {
			t.Errorf("%s: expected %d, got %d ok=%v", tag, want, got, ok)
		}
	}

	for _, values := range [][]string{
		{`W/"1"`}, {`*`}, {`"1", "2"`}, {`"1"`, `"2"`}, {`1`}, {`""`}, {`"0"`}, {`"-1"`},
		{`"abc"`}, {`"1.5"`}, {`"1`}, {`"99999999999999999999"`}, {`"+1"`},
	} {
		if _, ok := parseVersionETag(values); ok {
			t.Errorf("%q: expected to be rejected", values)
		}
	}
}

// concurrencyTarget is one protected resource's handler pair plus a fresh
// resource, so every case below runs against both /organisation and
// /organisation/settings.
type concurrencyTarget struct {
	name   string
	get    http.HandlerFunc
	patch  http.HandlerFunc
	body   string // a valid PATCH body
	orgID  uuid.UUID
	stored func() string // the field the PATCH body changes, as stored
}

func concurrencyTargets(t *testing.T) []concurrencyTarget {
	t.Helper()

	organisations := newFakeOrganisationRepository()
	organisationService := NewOrganisationService(organisations, newFakeSettingsRepository())
	organisation, err := organisationService.Create(context.Background(), "Acme Ltd")
	if err != nil {
		t.Fatalf("create organisation: %v", err)
	}
	organisationHandler := NewOrganisationHandler(organisationService)

	settingsRepository := newFakeSettingsRepository()
	settingsOrgID := uuid.New()
	seedSettings(t, settingsRepository, settingsOrgID)
	settingsHandler := NewSettingsHandler(NewSettingsService(settingsRepository))

	return []concurrencyTarget{
		{
			name: "organisation", get: organisationHandler.GetCurrent, patch: organisationHandler.Update,
			body: `{"phone":"0200"}`, orgID: organisation.ID,
			stored: func() string {
				o := organisations.organisations[organisation.ID]
				if o.Phone == nil {
					return ""
				}
				return *o.Phone
			},
		},
		{
			name: "settings", get: settingsHandler.GetCurrent, patch: settingsHandler.Update,
			body: `{"invoicePrefix":"NEW-"}`, orgID: settingsOrgID,
			stored: func() string { return settingsRepository.settings[settingsOrgID].InvoicePrefix },
		},
	}
}

func serve(handler http.HandlerFunc, method string, orgID uuid.UUID, body string, ifMatch ...string) *httptest.ResponseRecorder {
	request := httptest.NewRequest(method, "/", bytes.NewBufferString(body))
	request.Header.Set("Content-Type", "application/json")
	for _, value := range ifMatch {
		request.Header.Add("If-Match", value)
	}
	request = withAuthenticatedOrganisation(request, orgID)

	recorder := httptest.NewRecorder()
	handler(recorder, request)

	return recorder
}

func errorCodeOf(t *testing.T, recorder *httptest.ResponseRecorder) string {
	t.Helper()

	var body httpx.ErrorBody
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error body: %v (body: %s)", err, recorder.Body.String())
	}

	return body.Error.Code
}

func TestOptimisticConcurrency_GetReturnsETagAndPatchReturnsNewOne(t *testing.T) {
	for _, target := range concurrencyTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			get := serve(target.get, http.MethodGet, target.orgID, "")
			if get.Code != http.StatusOK || get.Header().Get("ETag") != `"1"` {
				t.Fatalf(`expected 200 with ETag "1", got %d ETag=%q`, get.Code, get.Header().Get("ETag"))
			}

			patch := serve(target.patch, http.MethodPatch, target.orgID, target.body, get.Header().Get("ETag"))
			if patch.Code != http.StatusOK || patch.Header().Get("ETag") != `"2"` {
				t.Fatalf(`expected 200 with ETag "2", got %d ETag=%q (body: %s)`, patch.Code, patch.Header().Get("ETag"), patch.Body.String())
			}

			if again := serve(target.get, http.MethodGet, target.orgID, ""); again.Header().Get("ETag") != `"2"` {
				t.Errorf(`expected a subsequent GET to return ETag "2", got %q`, again.Header().Get("ETag"))
			}

			// The version is an HTTP concern only — never in the JSON body.
			var fields map[string]any
			_ = json.Unmarshal(patch.Body.Bytes(), &fields)
			if _, ok := fields["version"]; ok {
				t.Error("expected no version field in the JSON body")
			}
		})
	}
}

func TestOptimisticConcurrency_MissingIfMatchIs428(t *testing.T) {
	for _, target := range concurrencyTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			before := target.stored()

			for _, ifMatch := range [][]string{nil, {""}, {"   "}} {
				recorder := serve(target.patch, http.MethodPatch, target.orgID, target.body, ifMatch...)
				if recorder.Code != http.StatusPreconditionRequired || errorCodeOf(t, recorder) != "precondition_required" {
					t.Errorf("If-Match %q: expected 428 precondition_required, got %d (body: %s)", ifMatch, recorder.Code, recorder.Body.String())
				}
			}

			if target.stored() != before {
				t.Error("expected nothing to be written")
			}
		})
	}
}

func TestOptimisticConcurrency_StaleIfMatchIs412AndWritesNothing(t *testing.T) {
	for _, target := range concurrencyTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			// Someone else saves first: version 1 -> 2.
			if first := serve(target.patch, http.MethodPatch, target.orgID, target.body, `"1"`); first.Code != http.StatusOK {
				t.Fatalf("first update: %d (body: %s)", first.Code, first.Body.String())
			}
			afterFirst := target.stored()

			stale := serve(target.patch, http.MethodPatch, target.orgID, `{}`, `"1"`)
			if stale.Code != http.StatusPreconditionFailed || errorCodeOf(t, stale) != "precondition_failed" {
				t.Fatalf("expected 412 precondition_failed, got %d (body: %s)", stale.Code, stale.Body.String())
			}
			if stale.Header().Get("ETag") != "" {
				t.Error("expected no ETag on a 412")
			}
			if target.stored() != afterFirst {
				t.Error("expected the stale request to write nothing")
			}

			// A version that was never issued is just as stale.
			if future := serve(target.patch, http.MethodPatch, target.orgID, `{}`, `"99"`); future.Code != http.StatusPreconditionFailed {
				t.Errorf("expected 412 for an unknown future version, got %d", future.Code)
			}
		})
	}
}

func TestOptimisticConcurrency_MalformedIfMatchIs400(t *testing.T) {
	for _, target := range concurrencyTargets(t) {
		t.Run(target.name, func(t *testing.T) {
			for _, ifMatch := range [][]string{
				{`W/"1"`}, {`*`}, {`"1", "2"`}, {`"1"`, `"1"`}, {`1`}, {`"abc"`}, {`"0"`},
			} {
				recorder := serve(target.patch, http.MethodPatch, target.orgID, target.body, ifMatch...)
				if recorder.Code != http.StatusBadRequest || errorCodeOf(t, recorder) != httpx.CodeInvalidRequest {
					t.Errorf("If-Match %q: expected 400 invalid_request, got %d (body: %s)", ifMatch, recorder.Code, recorder.Body.String())
				}
			}

			if get := serve(target.get, http.MethodGet, target.orgID, ""); get.Header().Get("ETag") != `"1"` {
				t.Errorf("expected nothing written (ETag still \"1\"), got %q", get.Header().Get("ETag"))
			}
		})
	}
}
