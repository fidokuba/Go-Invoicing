package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestWriteError_StandardEnvelope proves the representative status codes
// this API uses (Milestone 8 Part 2 section 25) all produce the same
// stable {error: {code, message}} JSON shape with the correct
// Content-Type.
func TestWriteError_StandardEnvelope(t *testing.T) {
	cases := []struct {
		name    string
		status  int
		code    string
		message string
	}{
		{"400", http.StatusBadRequest, CodeInvalidRequest, "invalid request body"},
		{"401", http.StatusUnauthorized, CodeUnauthorized, "unauthorized"},
		{"403", http.StatusForbidden, CodeForbidden, "forbidden"},
		{"404", http.StatusNotFound, "invoice_not_found", "invoice not found"},
		{"409", http.StatusConflict, CodeConflict, "invoice has already been sent"},
		{"415", http.StatusUnsupportedMediaType, CodeUnsupportedMediaType, "Content-Type must be application/json"},
		{"500", http.StatusInternalServerError, CodeInternalError, "internal server error"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder := httptest.NewRecorder()
			WriteError(recorder, tc.status, tc.code, tc.message)

			if recorder.Code != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, recorder.Code)
			}

			if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
				t.Errorf("expected Content-Type application/json, got %q", contentType)
			}

			var body ErrorBody
			if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
				t.Fatalf("decode error body: %v", err)
			}

			if body.Error.Code != tc.code {
				t.Errorf("expected code %q, got %q", tc.code, body.Error.Code)
			}

			if body.Error.Message != tc.message {
				t.Errorf("expected message %q, got %q", tc.message, body.Error.Message)
			}
		})
	}
}

func TestWriteJSON_SetsContentTypeAndStatus(t *testing.T) {
	recorder := httptest.NewRecorder()
	WriteJSON(recorder, http.StatusCreated, map[string]string{"id": "abc"})

	if recorder.Code != http.StatusCreated {
		t.Fatalf("expected status %d, got %d", http.StatusCreated, recorder.Code)
	}

	if contentType := recorder.Header().Get("Content-Type"); contentType != "application/json" {
		t.Errorf("expected Content-Type application/json, got %q", contentType)
	}

	var body map[string]string
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode body: %v", err)
	}

	if body["id"] != "abc" {
		t.Errorf(`expected {"id":"abc"}, got %v`, body)
	}
}
