package admin

import (
	"errors"
	"net/http"

	"go-invoicing/internal/httpx"
)

// AuthHandler owns the HTTP-specific concerns for authentication:
// decoding the login request, calling AuthService, and translating the
// result into a response. It holds no SQL and no password/token logic of
// its own, and never logs the raw session token, the password, or a
// password hash — the token appears exactly once, in the successful
// response body, and nowhere else.
//
// There is no authentication middleware yet (that is Part 3): this
// handler only issues credentials, it does not consume them.
type AuthHandler struct {
	service *AuthService
}

func NewAuthHandler(service *AuthService) *AuthHandler {
	return &AuthHandler{
		service: service,
	}
}

// Login handles POST /auth/login. Every invalid-credential case —
// unknown email, wrong password, inactive account — reaches here as the
// same ErrInvalidCredentials and is mapped to the same generic 401 with
// the same body, by design: this handler cannot leak, through response
// content or status code, which of the three actually happened.
func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var request LoginRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	result, err := h.service.Login(r.Context(), request.Email, request.Password)
	if err != nil {
		if errors.Is(err, ErrLoginEmailRequired) || errors.Is(err, ErrLoginPasswordRequired) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrInvalidCredentials) {
			httpx.WriteError(w, http.StatusUnauthorized, httpx.CodeUnauthorized, "invalid email or password")
			return
		}

		httpx.WriteInternalError(w, r, "auth.login", err)
		return
	}

	response := toLoginResponse(result)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// Logout handles POST /auth/logout (Milestone 8 Part 3) — authentication
// required. It revokes exactly the session the presented bearer token
// resolved to (identity.SessionID, attached by AuthMiddleware.RequireAuth
// — never re-derived from request data, and never accepted as a body
// field), so the current token stops working immediately while every
// other active session for the same user is left untouched.
//
// 204 No Content: a successful logout has no resource to return — there
// is nothing about "you are now logged out" worth serializing, unlike
// every other mutating endpoint in this API, which returns the
// resource it just created or changed.
func (h *AuthHandler) Logout(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if err := h.service.Logout(r.Context(), identity.SessionID); err != nil {
		httpx.WriteInternalError(w, r, "auth.logout", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
