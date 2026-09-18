package admin

import (
	"time"

	"github.com/google/uuid"
)

// SessionTTL is the fixed lifetime of a session, measured from the moment
// it is created. There is no sliding/rolling expiration: a session
// created now expires exactly SessionTTL later, no matter how often it is
// used in between. Kept as a single named constant, rather than a literal
// scattered across AuthService and its tests, so both read and assert
// against the same value.
const SessionTTL = 24 * time.Hour

// Session is a server-side record of a successful login. The raw bearer
// token handed to the client is never stored — TokenHash is its SHA-256
// hash (see hashSessionToken in session_token.go); there is no field on
// this type that could ever hold the raw value.
//
// A session belongs to a User, not directly to an organisation:
// organisation identity is resolved via User.OrganisationID once a
// session has been validated. There is deliberately no OrganisationID
// field here.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	TokenHash string
	CreatedAt time.Time
	ExpiresAt time.Time
	RevokedAt *time.Time
}

// IsValid reports whether the session is neither revoked nor expired as
// of now. Nothing in this milestone calls Revoke or sets RevokedAt yet
// (logout/revocation is out of scope for Part 2), but Part 3's
// authentication middleware is expected to call IsValid immediately after
// SessionRepository.GetByTokenHash, before trusting a session.
func (s *Session) IsValid(now time.Time) bool {
	if s.RevokedAt != nil {
		return false
	}

	return now.Before(s.ExpiresAt)
}
