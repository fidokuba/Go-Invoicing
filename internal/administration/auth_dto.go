package admin

import "time"

// LoginRequest is the shape a client POSTs to /auth/login. There is no
// organisationId field: authentication resolves the user (and therefore
// their organisation) from email alone, now that email is globally
// unique (Milestone 4 Part 2).
type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// LoginResponse is the shape returned on a successful login. Token is the
// raw session token — the only place it is ever exposed; it is not
// stored anywhere and cannot be retrieved again. User reuses UserResponse,
// which has no Password or PasswordHash field at all.
type LoginResponse struct {
	Token     string       `json:"token"`
	ExpiresAt string       `json:"expiresAt"`
	User      UserResponse `json:"user"`
}

// toLoginResponse maps a successful LoginResult onto the API's response
// shape.
func toLoginResponse(result *LoginResult) LoginResponse {
	return LoginResponse{
		Token:     result.Token,
		ExpiresAt: result.Session.ExpiresAt.UTC().Format(time.RFC3339),
		User:      toUserResponse(result.User),
	}
}
