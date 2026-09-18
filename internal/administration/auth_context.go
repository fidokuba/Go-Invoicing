package admin

import (
	"context"
	"net/http"

	"github.com/google/uuid"
)

// AuthenticatedUser is the identity AuthMiddleware attaches to an
// authenticated request's context. It deliberately carries only what
// downstream code needs to make authorization/tenant decisions — not the
// full User. In particular, PasswordHash (and every other credential or
// audit field) must never be copied in here: context.Context flows
// through every middleware, handler and service call for the rest of the
// request, so keeping credential material out of it is a deliberate
// defense-in-depth choice, not an oversight.
//
// UserID and Role are what Part 5's role-based authorization will need.
// OrganisationID is the sole source of tenant identity for every
// protected endpoint (Milestone 4 Part 4, via RequireAuthenticatedUser
// below) — client-supplied organisationId query parameters are no longer
// read on any protected route.
type AuthenticatedUser struct {
	UserID         uuid.UUID
	OrganisationID uuid.UUID
	Role           string
}

// authContextKey is an unexported type so no other package's
// context.WithValue call can ever collide with this key, even
// accidentally — the well-known reason to avoid a bare string or int
// context key.
type authContextKey int

// authenticatedUserContextKey is the sole key this package stores in
// context.Context.
const authenticatedUserContextKey authContextKey = 0

// WithAuthenticatedUser returns a new context carrying user as the
// request's authenticated identity. Called by AuthMiddleware.RequireAuth
// once a bearer token has been fully validated.
func WithAuthenticatedUser(ctx context.Context, user AuthenticatedUser) context.Context {
	return context.WithValue(ctx, authenticatedUserContextKey, user)
}

// AuthenticatedUserFromContext returns the request's authenticated
// identity, if any. ok is false when no identity was ever attached (e.g.
// a public route, or a context that didn't pass through
// AuthMiddleware.RequireAuth) — callers must check it rather than assume
// a zero-value AuthenticatedUser is meaningful.
func AuthenticatedUserFromContext(ctx context.Context) (AuthenticatedUser, bool) {
	user, ok := ctx.Value(authenticatedUserContextKey).(AuthenticatedUser)
	return user, ok
}

// RequireAuthenticatedUser is the single entry point every protected
// handler (in this package or any other) uses to resolve tenant identity.
// It reads AuthenticatedUser from r's context and, when present, returns
// it for the caller to use — in particular its OrganisationID, which must
// be the only source of organisation identity for any tenant-scoped
// operation (Milestone 4 Part 4). It never inspects, parses, or falls
// back to a client-supplied organisationId of any kind.
//
// When no identity is present — which should only happen if a handler is
// reached without going through AuthMiddleware.RequireAuth, e.g. a
// handler unit test invoking the handler function directly — this fails
// closed: it writes the exact same generic 401 response
// AuthMiddleware itself uses (via unauthorized), and returns ok=false so
// the caller returns immediately without doing any tenant-scoped work.
func RequireAuthenticatedUser(w http.ResponseWriter, r *http.Request) (AuthenticatedUser, bool) {
	identity, ok := AuthenticatedUserFromContext(r.Context())
	if !ok {
		unauthorized(w)
		return AuthenticatedUser{}, false
	}

	return identity, true
}
