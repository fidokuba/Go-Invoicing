package admin

import (
	"encoding/json"
	"errors"
	"net/http"
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

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.service.Login(r.Context(), request.Email, request.Password)
	if err != nil {
		if errors.Is(err, ErrLoginEmailRequired) || errors.Is(err, ErrLoginPasswordRequired) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if errors.Is(err, ErrInvalidCredentials) {
			http.Error(w, "invalid email or password", http.StatusUnauthorized)
			return
		}

		http.Error(w, "failed to log in", http.StatusInternalServerError)
		return
	}

	response := toLoginResponse(result)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
