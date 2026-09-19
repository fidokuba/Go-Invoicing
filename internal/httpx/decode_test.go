package httpx

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type decodeTarget struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func newDecodeRequest(body, contentType string) (*httptest.ResponseRecorder, *http.Request) {
	request := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(body))
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	return httptest.NewRecorder(), request
}

func TestDecodeJSON_ValidApplicationJSON(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","count":3}`, "application/json")

	var dst decodeTarget
	if !DecodeJSON(recorder, request, &dst) {
		t.Fatalf("expected decode to succeed, got status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	if dst.Name != "Widget" || dst.Count != 3 {
		t.Errorf("expected {Widget 3}, got %+v", dst)
	}
}

func TestDecodeJSON_ApplicationJSONWithCharsetParameter(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","count":3}`, "application/json; charset=utf-8")

	var dst decodeTarget
	if !DecodeJSON(recorder, request, &dst) {
		t.Fatalf("expected a charset parameter not to affect acceptance, got status %d (body: %s)", recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSON_MissingContentType(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","count":3}`, "")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail with no Content-Type header")
	}

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnsupportedMediaType, recorder.Code, recorder.Body.String())
	}

	assertErrorCode(t, recorder, CodeUnsupportedMediaType)
}

func TestDecodeJSON_TextPlainContentType(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","count":3}`, "text/plain")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for text/plain")
	}

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnsupportedMediaType, recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSON_FormURLEncodedContentType(t *testing.T) {
	recorder, request := newDecodeRequest(`name=Widget&count=3`, "application/x-www-form-urlencoded")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for application/x-www-form-urlencoded")
	}

	if recorder.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnsupportedMediaType, recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSON_MalformedJSON(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":`, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for malformed JSON")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	assertErrorCode(t, recorder, CodeInvalidRequest)
}

func TestDecodeJSON_UnknownField(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","unexpectedField":"silently ignored?"}`, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for an unknown field")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}

	assertErrorCode(t, recorder, CodeInvalidRequest)
}

// TestDecodeJSON_TrailingJSONValue proves a second JSON value after the
// first is rejected, not silently ignored the way a bare
// json.Decoder.Decode call would (it only ever reads the first value and
// never looks further).
func TestDecodeJSON_TrailingJSONValue(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget"} {"another":"object"}`, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail when trailing JSON follows the first value")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSON_EmptyBody(t *testing.T) {
	recorder, request := newDecodeRequest(``, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for an empty body")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestDecodeJSON_WrongFieldType(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"Widget","count":"not a number"}`, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for a wrong field type")
	}

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestDecodeJSON_OversizedBody proves a body larger than
// MaxRequestBodyBytes is rejected with 413, rather than read in full.
func TestDecodeJSON_OversizedBody(t *testing.T) {
	oversized := `{"name":"` + strings.Repeat("a", MaxRequestBodyBytes+1) + `"}`
	recorder, request := newDecodeRequest(oversized, "application/json")

	var dst decodeTarget
	if DecodeJSON(recorder, request, &dst) {
		t.Fatal("expected decode to fail for an oversized body")
	}

	if recorder.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusRequestEntityTooLarge, recorder.Code, recorder.Body.String())
	}

	assertErrorCode(t, recorder, CodeRequestTooLarge)
}

// TestDecodeJSON_DuplicateKeys documents (Milestone 8 Part 2 section 19)
// that encoding/json's own last-value-wins behaviour for duplicate
// object keys is left as-is — this is not a defect DecodeJSON attempts
// to fix, and this test exists to make that a deliberate, visible
// decision rather than silent behaviour nobody verified.
func TestDecodeJSON_DuplicateKeys(t *testing.T) {
	recorder, request := newDecodeRequest(`{"name":"First","name":"Second","count":1}`, "application/json")

	var dst decodeTarget
	if !DecodeJSON(recorder, request, &dst) {
		t.Fatalf("expected decode to succeed (duplicate keys are not rejected), got status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	if dst.Name != "Second" {
		t.Errorf("expected the last duplicate key's value to win (encoding/json's own behaviour), got %q", dst.Name)
	}
}

// TestDecodeJSON_NeverLeaksDecoderInternals sweeps every failure mode
// above and asserts none of their response bodies mention encoding/json
// package internals (struct field/type names, "json:" prefixed errors) —
// only this package's own fixed, safe messages.
func TestDecodeJSON_NeverLeaksDecoderInternals(t *testing.T) {
	cases := []struct {
		name        string
		body        string
		contentType string
	}{
		{"malformed", `{"name":`, "application/json"},
		{"unknown field", `{"name":"x","unexpectedField":"y"}`, "application/json"},
		{"wrong type", `{"name":"x","count":"y"}`, "application/json"},
		{"empty", ``, "application/json"},
		{"trailing", `{"name":"x"} {"y":"z"}`, "application/json"},
	}

	leakedSubstrings := []string{"json:", "decodeTarget", "unexpectedField", "cannot unmarshal"}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			recorder, request := newDecodeRequest(tc.body, tc.contentType)

			var dst decodeTarget
			if DecodeJSON(recorder, request, &dst) {
				t.Fatalf("expected decode to fail for case %q", tc.name)
			}

			responseBody := recorder.Body.String()
			for _, leaked := range leakedSubstrings {
				if strings.Contains(responseBody, leaked) {
					t.Errorf("expected no decoder-internal detail, but response contained %q: %s", leaked, responseBody)
				}
			}
		})
	}
}

// assertErrorCode decodes recorder's body as the standard error envelope
// and asserts its Code matches want.
func assertErrorCode(t *testing.T, recorder *httptest.ResponseRecorder, want string) {
	t.Helper()

	var body ErrorBody
	if err := json.NewDecoder(recorder.Body).Decode(&body); err != nil {
		t.Fatalf("decode error body: %v", err)
	}

	if body.Error.Code != want {
		t.Errorf("expected error code %q, got %q", want, body.Error.Code)
	}
}
