package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"go-invoicing/internal/httpx"
)

// TestErrorContract_MatchesOpenAPISchema is Milestone 8 Part 5 section
// 11: it calls the real httpx.WriteError (the single function every
// handler in this application uses to produce an error body) for one
// representative case of each status code the spec documents, and
// validates the actual written response body against the ErrorBody
// schema. There is deliberately one shared schema for every status
// code (per the spec's own design — see api/openapi.yaml's Error
// response components) rather than a separate schema per code, so this
// loop proves the one envelope shape holds across the full set rather
// than proving eight different things.
func TestErrorContract_MatchesOpenAPISchema(t *testing.T) {
	doc := loadSpec(t)

	cases := []struct {
		status  int
		code    string
		message string
	}{
		{http.StatusBadRequest, httpx.CodeValidationFailed, "a field failed validation"},
		{http.StatusUnauthorized, httpx.CodeUnauthorized, "unauthorized"},
		{http.StatusForbidden, httpx.CodeForbidden, "forbidden"},
		{http.StatusNotFound, "invoice_not_found", "invoice not found"},
		{http.StatusConflict, "invoice_already_sent", "invoice has already been sent"},
		{http.StatusRequestEntityTooLarge, httpx.CodeRequestTooLarge, "request body exceeds the maximum allowed size"},
		{http.StatusUnsupportedMediaType, httpx.CodeUnsupportedMediaType, "Content-Type must be application/json"},
		{http.StatusInternalServerError, httpx.CodeInternalError, "internal server error"},
	}

	for _, c := range cases {
		t.Run(http.StatusText(c.status), func(t *testing.T) {
			recorder := httptest.NewRecorder()
			httpx.WriteError(recorder, c.status, c.code, c.message)

			if recorder.Code != c.status {
				t.Fatalf("expected status %d, got %d", c.status, recorder.Code)
			}
			if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", contentType)
			}

			var generic any
			if err := json.Unmarshal(recorder.Body.Bytes(), &generic); err != nil {
				t.Fatalf("unmarshal error body: %v", err)
			}

			validateAgainstSchema(t, doc, "ErrorBody", generic)

			body, ok := generic.(map[string]any)
			if !ok {
				t.Fatalf("expected a JSON object, got %T", generic)
			}
			errorField, ok := body["error"].(map[string]any)
			if !ok {
				t.Fatalf(`expected an "error" object, got %v`, body["error"])
			}
			if _, ok := errorField["code"].(string); !ok {
				t.Errorf("expected error.code to be a string, got %v", errorField["code"])
			}
			if _, ok := errorField["message"].(string); !ok {
				t.Errorf("expected error.message to be a string, got %v", errorField["message"])
			}
		})
	}
}
