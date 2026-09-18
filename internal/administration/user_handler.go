package admin

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/google/uuid"
)

// UserHandler owns the HTTP-specific concerns for users: decoding
// requests, calling the service, translating errors into status codes,
// and encoding responses. It holds no SQL, no business rules, and never
// touches a plaintext or hashed password directly — that's entirely
// UserService/hashPassword's concern.
//
// Both routes are protected (Milestone 4 Part 4) and role-gated
// (Milestone 4 Part 5). Create is additionally wrapped in RequireRole at
// the route-registration site in app.go — see UserService.Create's doc
// comment for why the privilege-escalation rule is enforced again inside
// the service rather than relying on that route gate alone.
type UserHandler struct {
	service *UserService
}

func NewUserHandler(
	service *UserService,
) *UserHandler {
	return &UserHandler{
		service: service,
	}
}

// Create handles POST /users. Organisation identity and the acting
// caller's identity/role both come exclusively from the authenticated
// context — never from client input of any kind.
func (h *UserHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request CreateUserRequest

	if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
		http.Error(w, "invalid request body", http.StatusBadRequest)
		return
	}

	actor := CreateUserActor{
		UserID: identity.UserID,
		Role:   identity.Role,
	}

	user, err := h.service.Create(r.Context(), identity.OrganisationID, actor, request)
	if err != nil {
		if errors.Is(err, ErrUserNameRequired) ||
			errors.Is(err, ErrUserEmailRequired) ||
			errors.Is(err, ErrUserPasswordRequired) ||
			errors.Is(err, ErrUserPasswordTooShort) ||
			errors.Is(err, ErrUserRoleInvalid) {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}

		if errors.Is(err, ErrUserActorNotPermittedToCreateUsers) ||
			errors.Is(err, ErrUserRoleAssignmentNotPermitted) {
			forbidden(w)
			return
		}

		if errors.Is(err, ErrOrganisationNotFound) {
			http.Error(w, "organisation not found", http.StatusNotFound)
			return
		}

		if errors.Is(err, ErrUserEmailAlreadyExists) {
			http.Error(w, err.Error(), http.StatusConflict)
			return
		}

		http.Error(w, "failed to create user", http.StatusInternalServerError)
		return
	}

	response := toUserResponse(user)

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}

// GetByID handles GET /users/{id}. The requested user ID is
// client-supplied and stays that way, and the organisation it's looked up
// within comes exclusively from the authenticated caller — a user from
// Organisation A requesting a user belonging to Organisation B gets the
// same 404 UserService.GetByID already returns for any nonexistent user,
// never a hint that the user exists elsewhere.
//
// Self-vs-other (Milestone 4 Part 5): every authenticated user may
// retrieve their own record; retrieving another user in the same
// organisation additionally requires admin or manager. This check is
// resource-specific (it compares the path ID to the caller's own ID) so
// it lives here rather than in generic role middleware.
func (h *UserHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "invalid user ID", http.StatusBadRequest)
		return
	}

	if id != identity.UserID && identity.Role != UserRoleAdmin && identity.Role != UserRoleManager {
		forbidden(w)
		return
	}

	user, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			http.Error(w, "user not found", http.StatusNotFound)
			return
		}

		http.Error(w, "failed to get user", http.StatusInternalServerError)
		return
	}

	response := toUserResponse(user)

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(response); err != nil {
		http.Error(w, "failed to encode response", http.StatusInternalServerError)
	}
}
