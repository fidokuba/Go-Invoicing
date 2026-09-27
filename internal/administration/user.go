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

// CurrentTermsVersion identifies the version of the Terms & Conditions
// presented to a user at signup. RegistrationService.Register stamps
// every newly-created user's TermsVersion with this value, so
// TermsAcceptedAt/TermsVersion together record exactly which wording a
// given user agreed to. Bump this string whenever the terms text
// meaningfully changes — it is not read from the terms page itself,
// there is no automatic link between the two.
const CurrentTermsVersion = "2026-09-27"

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

	// TermsAcceptedAt/TermsVersion record when — and to which version of
	// the Terms & Conditions — this user agreed at signup (Register
	// requires agreement; see ErrTermsNotAccepted). Both are nil for
	// every user created before this tracking existed: there is no way
	// to know what, if anything, an existing user agreed to, so this is
	// deliberately not backfilled. Set only at creation — not
	// re-fetched by GetByID/GetByEmail/List, which have no need of it.
	TermsAcceptedAt *time.Time
	TermsVersion    *string
}

func (u *User) TableName() string {
	return "users"
}

func (u *User) IsAdmin() bool {
	return u.Role == UserRoleAdmin
}
