package admin

import "time"

// CreateUserRequest is the shape a client may POST to create a user. It
// deliberately exposes only what a caller is allowed to set — not ID,
// IsActive, LastLogin, CreatedAt/UpdatedAt/DeletedAt, all of which are
// server/domain responsibilities. Role is optional: UserService.Create
// defaults a blank Role to UserRoleUser.
type CreateUserRequest struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UserResponse is the shape returned to clients. It has no Password or
// PasswordHash field at all — not omitempty, not a pointer set to nil,
// structurally absent — so there is no representable value of this type
// that could ever serialize a credential. Never add one back without
// re-reading that sentence.
type UserResponse struct {
	ID             string  `json:"id"`
	OrganisationID string  `json:"organisationId"`
	Name           string  `json:"name"`
	Email          string  `json:"email"`
	Role           string  `json:"role"`
	IsActive       bool    `json:"isActive"`
	LastLogin      *string `json:"lastLogin,omitempty"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

// toUserResponse maps the internal domain model onto the API's response
// shape. It takes *User (which does carry PasswordHash) only to read the
// public fields off it — it never touches PasswordHash, by construction:
// UserResponse has nowhere to put it.
func toUserResponse(u *User) UserResponse {
	var lastLogin *string
	if u.LastLogin != nil {
		s := u.LastLogin.UTC().Format(time.RFC3339)
		lastLogin = &s
	}

	return UserResponse{
		ID:             u.ID.String(),
		OrganisationID: u.OrganisationID.String(),
		Name:           u.Name,
		Email:          u.Email,
		Role:           u.Role,
		IsActive:       u.IsActive,
		LastLogin:      lastLogin,
		CreatedAt:      u.CreatedAt.UTC().Format(time.RFC3339),
		UpdatedAt:      u.UpdatedAt.UTC().Format(time.RFC3339),
	}
}
