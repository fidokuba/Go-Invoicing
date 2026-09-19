package admin

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/google/uuid"

	"go-invoicing/internal/httpx"
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
	if !httpx.DecodeJSON(w, r, &request) {
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
			errors.Is(err, ErrUserEmailInvalid) ||
			errors.Is(err, ErrUserPasswordRequired) ||
			errors.Is(err, ErrUserPasswordTooShort) ||
			errors.Is(err, ErrUserRoleInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		if errors.Is(err, ErrUserActorNotPermittedToCreateUsers) ||
			errors.Is(err, ErrUserRoleAssignmentNotPermitted) {
			forbidden(w)
			return
		}

		if errors.Is(err, ErrOrganisationNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "organisation_not_found", "organisation not found")
			return
		}

		if errors.Is(err, ErrUserEmailAlreadyExists) {
			httpx.WriteError(w, http.StatusConflict, "email_already_exists", err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to create user")
		return
	}

	response := toUserResponse(user)

	httpx.WriteJSON(w, http.StatusCreated, response)
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
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid user ID")
		return
	}

	if id != identity.UserID && identity.Role != UserRoleAdmin && identity.Role != UserRoleManager {
		forbidden(w)
		return
	}

	user, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "user_not_found", "user not found")
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to get user")
		return
	}

	response := toUserResponse(user)

	httpx.WriteJSON(w, http.StatusOK, response)
}

// userSortFields is the public sort-field allow-list for GET /users,
// validated by httpx.ParseSortOrder before List ever runs — see
// userSortColumns in user_repository_postgres.go for how each of these
// maps onto an actual SQL column.
var userSortFields = []string{"email", "role", "createdAt"}

// List handles GET /users (Milestone 8 Part 3) — organisation user/team
// management, so it is role-gated at the route-registration site
// (RequireRole(UserRoleAdmin, UserRoleManager)), the same gate POST
// /users already uses; an ordinary user reaches this handler only if
// that gate is bypassed in a test, and RequireAuthenticatedUser below
// still fails closed with 401 in that case. Default sort is createdAt
// descending — newest team member first, the most useful default for a
// team-management view.
func (h *UserHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if !httpx.RejectUnknownQueryParams(w, r, "limit", "offset", "role", "active", "sort", "order") {
		return
	}

	limit, offset, ok := httpx.ParseLimitOffset(w, r)
	if !ok {
		return
	}

	sort, order, ok := httpx.ParseSortOrder(w, r, userSortFields, "createdAt", "desc")
	if !ok {
		return
	}

	query := r.URL.Query()
	role, _ := httpx.OptionalQueryParam(query, "role")

	var active *bool
	if raw, present := httpx.OptionalQueryParam(query, "active"); present {
		parsed, err := strconv.ParseBool(raw)
		if err != nil {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, "active must be true or false")
			return
		}
		active = &parsed
	}

	filter := UserListFilter{
		Role:   role,
		Active: active,
		Sort:   sort,
		Order:  order,
		Limit:  limit,
		Offset: offset,
	}

	users, total, err := h.service.List(r.Context(), identity.OrganisationID, filter)
	if err != nil {
		if errors.Is(err, ErrUserRoleFilterInvalid) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteError(w, http.StatusInternalServerError, httpx.CodeInternalError, "failed to list users")
		return
	}

	items := make([]UserResponse, 0, len(users))
	for _, u := range users {
		items = append(items, toUserResponse(u))
	}

	httpx.WriteJSON(w, http.StatusOK, httpx.NewListResponse(items, limit, offset, total))
}
