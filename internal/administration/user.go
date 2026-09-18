package admin

import (
	"time"

	"github.com/google/uuid"
)

// User roles. Named constants rather than scattered string literals —
// same reasoning as InvoiceStatus* in the invoice package: this is about
// to have real role-dependent logic (UserService.Create validates
// against this exact set), so a typo in a literal should be a compile
// error, not a silent acceptance of an invalid role.
const (
	UserRoleAdmin   = "admin"
	UserRoleManager = "manager"
	UserRoleUser    = "user"
)

// DeletedAt is a pointer because that column is nullable in the users
// table; every other field here is NOT NULL except LastLogin, which is
// also nullable and already correctly a pointer.
type User struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	Name           string
	Email          string
	PasswordHash   string
	Role           string // one of the UserRole* constants above
	IsActive       bool
	LastLogin      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      *time.Time
}

func (u *User) TableName() string {
	return "users"
}

func (u *User) IsAdmin() bool {
	return u.Role == UserRoleAdmin
}
