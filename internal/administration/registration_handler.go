package admin

import (
	"errors"
	"net/http"

	"go-invoicing/internal/httpx"
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
	if !httpx.DecodeJSON(w, r, &request) {
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
			errors.Is(err, ErrUserEmailInvalid) ||
			errors.Is(err, ErrUserPasswordRequired) ||
			errors.Is(err, ErrUserPasswordTooShort) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrUserEmailAlreadyExists) {
			httpx.WriteError(w, http.StatusConflict, "email_already_exists", err.Error())
			return
		}

		httpx.WriteInternalError(w, r, "registration.register", err)
		return
	}

	response := toRegisterResponse(result)

	httpx.WriteJSON(w, http.StatusCreated, response)
}
