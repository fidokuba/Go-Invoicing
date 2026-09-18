package admin

import "net/http"

// forbidden writes the single generic 403 response used for every
// role-related authorisation failure, regardless of which role was
// actually required — the same "one generic body, no internal detail"
// principle unauthorized (in auth_middleware.go) already applies to
// authentication failures. Never expose which role(s) were required or
// why the caller was denied.
func forbidden(w http.ResponseWriter) {
	http.Error(w, "forbidden", http.StatusForbidden)
}

// RequireRole returns a middleware that only allows a request through
// when the authenticated caller's role is one of roles. Allowed roles are
// explicit per call site — there is no encoded hierarchy (e.g. "admin
// implies manager") to reason about, and none is needed for this
// project's three roles and small number of role-gated routes.
//
// It reads AuthenticatedUser via RequireAuthenticatedUser rather than
// assuming the identity was already resolved: in normal request flow
// RequireRole is composed inside AuthMiddleware.RequireAuth (which has
// already attached the identity to the request's context by the time
// this runs), but calling it standalone — e.g. a test invoking a
// RequireRole-wrapped handler directly — must still fail closed with 401
// rather than silently allowing the request through.
//
// Authentication and authorisation stay distinct failure modes: a
// missing/invalid session is 401 (unauthorized, from
// RequireAuthenticatedUser); a valid session whose role isn't permitted
// is 403 (forbidden). Every 403 this produces has the same generic body —
// it never reveals which role(s) would have been accepted.
func RequireRole(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	allowed := make(map[string]struct{}, len(roles))
	for _, role := range roles {
		allowed[role] = struct{}{}
	}

	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			identity, ok := RequireAuthenticatedUser(w, r)
			if !ok {
				return
			}

			if _, permitted := allowed[identity.Role]; !permitted {
				forbidden(w)
				return
			}

			next(w, r)
		}
	}
}
