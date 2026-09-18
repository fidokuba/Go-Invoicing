package admin

import (
	"encoding/json"
	"errors"
	"net/http"
)

// RegistrationHandler owns the HTTP-specific concerns for registration:
// decoding the request, calling RegistrationService, and translating the
// result into a response. It is the only public route that can ever
// create an organisation or a user with role admin — everything else
// that creates a user is protected and role-gated (Milestone 4 Part 5).
type RegistrationHandler struct {
	service *RegistrationService
}

func NewRegistrationHandler(service *RegistrationService) *RegistrationHandler {
	return &RegistrationHandler{
		service: service,
	}
}

// Register handles POST /register. There is no authentication on this
// route — it's how an organisation and its first (admin) user come to
// exist in the first place — and no session is issued: a successful
// registration returns 201 with the created organisation and user, and
// the client must call POST /auth/login afterward to obtain a bearer
// token.
func (h *RegistrationHandler) Register(w http.ResponseWriter, r *http.Request) {
	var request RegisterRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	result, err := h.service.Register(
		r.Context(),
		request.Organisation.Name,
		request.User.Name,
		request.User.Email,
		request.User.Password,
	)
	if err != nil {
		if errors.Is(err, ErrOrganisationNameRequired) ||
			errors.Is(err, ErrUserNameRequired) ||
			errors.Is(err, ErrUserEmailRequired) ||
			errors.Is(err, ErrUserPasswordRequired) ||
			errors.Is(err, ErrUserPasswordTooShort) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if errors.Is(err, ErrUserEmailAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, "failed to register", http.StatusInternalServerError)
		return
	}

	response := toRegisterResponse(result)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
