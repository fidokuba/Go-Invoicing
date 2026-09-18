package admin

// RegisterOrganisationRequest is the nested organisation shape within
// RegisterRequest. It deliberately has only a name — everything else an
// Organisation can hold is optional and unrelated to bootstrapping an
// account.
type RegisterOrganisationRequest struct {
	Name string `json:"name"`
}

// RegisterUserRequest is the nested user shape within RegisterRequest.
// There is deliberately no Role field: the first user's role is always
// UserRoleAdmin, decided server-side by RegistrationService.Register —
// an anonymous caller has no way to request any other initial role,
// because the wire format has nowhere to put one.
type RegisterUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// RegisterRequest is the shape a client POSTs to /register.
type RegisterRequest struct {
	Organisation RegisterOrganisationRequest `json:"organisation"`
	User         RegisterUserRequest         `json:"user"`
}

// RegisterResponse is the shape returned on successful registration. It
// reuses OrganisationResponse and UserResponse — the latter already has
// no Password or PasswordHash field at all. There is no token: Register
// does not authenticate the caller (see RegistrationService.Register),
// so a client must call POST /auth/login afterward.
type RegisterResponse struct {
	Organisation OrganisationResponse `json:"organisation"`
	User         UserResponse         `json:"user"`
}

// toRegisterResponse maps a successful RegistrationResult onto the API's
// response shape.
func toRegisterResponse(result *RegistrationResult) RegisterResponse {
	return RegisterResponse{
		Organisation: toOrganisationResponse(result.Organisation),
		User:         toUserResponse(result.User),
	}
}
