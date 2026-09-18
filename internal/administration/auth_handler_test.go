package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func newTestAuthHandler(t *testing.T) (*AuthHandler, *authTestFixture) {
	t.Helper()

	f := newAuthTestFixture(t)
	handler := NewAuthHandler(f.service)
	return handler, f
}

func TestAuthHandler_Login_Success(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{"email":"` + f.activeEmail + `","password":"` + f.activePassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response LoginResponse
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	if response.Token == "" {
		t.Error("expected a non-empty token in the response")
	}

	if response.ExpiresAt == "" {
		t.Error("expected a non-empty expiresAt in the response")
	}

	if response.User.Email != f.activeEmail {
		t.Errorf("expected user email %q, got %q", f.activeEmail, response.User.Email)
	}

	if response.User.ID == "" {
		t.Error("expected a non-empty user ID in the response")
	}
}

func TestAuthHandler_Login_ResponseContainsNoCredentialFields(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{"email":"` + f.activeEmail + `","password":"` + f.activePassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	raw := strings.ToLower(recorder.Body.String())
	if strings.Contains(raw, "passwordhash") || strings.Contains(raw, `"password"`) {
		t.Errorf("expected response body to contain no password-related fields, got %s", recorder.Body.String())
	}
}

func TestAuthHandler_Login_MalformedJSON(t *testing.T) {
	handler, _ := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestAuthHandler_Login_MissingEmail(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{"password":"` + f.activePassword + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

func TestAuthHandler_Login_MissingPassword(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{"email":"` + f.activeEmail + `"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusBadRequest, recorder.Code, recorder.Body.String())
	}
}

// TestAuthHandler_Login_InvalidCredentialCasesAreIndistinguishable is the
// crux security test for this handler: wrong password, unknown email, and
// an inactive account must all produce exactly the same HTTP status and
// exactly the same response body, so a caller (or an attacker) cannot
// learn which of the three actually happened.
func TestAuthHandler_Login_InvalidCredentialCasesAreIndistinguishable(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	scenarios := map[string]string{
		"wrong password": `{"email":"` + f.activeEmail + `","password":"totally the wrong password"}`,
		"unknown email":  `{"email":"nobody@example.com","password":"whatever it is"}`,
		"inactive user":  `{"email":"` + f.inactiveEmail + `","password":"` + f.inactivePassword + `"}`,
	}

	var referenceStatus int
	var referenceBody string

	for name, payload := range scenarios {
		body := bytes.NewBufferString(payload)
		request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
		recorder := httptest.NewRecorder()

		handler.Login(recorder, request)

		if recorder.Code != http.StatusUnauthorized {
			t.Errorf("%s: expected status %d, got %d (body: %s)", name, http.StatusUnauthorized, recorder.Code, recorder.Body.String())
		}

		if referenceStatus == 0 {
			referenceStatus = recorder.Code
			referenceBody = recorder.Body.String()
			continue
		}

		if recorder.Code != referenceStatus || recorder.Body.String() != referenceBody {
			t.Errorf(
				"%s: expected the same response as the other invalid-credential cases (status %d, body %q), got status %d, body %q",
				name, referenceStatus, referenceBody, recorder.Code, recorder.Body.String(),
			)
		}
	}
}

func TestAuthHandler_Login_ErrorResponsesDoNotLeakToken(t *testing.T) {
	handler, f := newTestAuthHandler(t)

	body := bytes.NewBufferString(`{"email":"` + f.activeEmail + `","password":"wrong password"}`)
	request := httptest.NewRequest(http.MethodPost, "/auth/login", body)
	recorder := httptest.NewRecorder()

	handler.Login(recorder, request)

	if strings.Contains(strings.ToLower(recorder.Body.String()), "token") {
		t.Errorf("expected a failed login response to contain no token field, got %s", recorder.Body.String())
	}
}
