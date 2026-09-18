package admin

import (
	"errors"
	"net/http"
	"strings"
	"time"
)

// AuthMiddleware validates the "Authorization: Bearer <token>" header on
// protected routes and, on success, attaches an AuthenticatedUser to the
// request's context before calling the downstream handler.
//
// It performs no writes of any kind: it never updates LastLogin, never
// extends a session's expiry, and never otherwise modifies session or
// user state. Authenticating a request is a pure read — the fixed,
// non-sliding session TTL AuthService.Login already established in
// Part 2 is not something this middleware may quietly change.
type AuthMiddleware struct {
	sessionRepository SessionRepository
	userRepository    UserRepository
}

// NewAuthMiddleware wires the repositories RequireAuth needs.
func NewAuthMiddleware(
	sessionRepository SessionRepository,
	userRepository UserRepository,
) *AuthMiddleware {
	return &AuthMiddleware{
		sessionRepository: sessionRepository,
		userRepository:    userRepository,
	}
}

// unauthorized writes the single generic 401 response used for every
// authentication failure, regardless of cause: a missing/malformed
// header, an unknown token, an expired or revoked session, a deleted
// user, and an inactive user are all indistinguishable to the caller —
// the same principle AuthService.Login already applies to failed logins,
// applied here to failed authentication. WWW-Authenticate: Bearer is the
// standard (RFC 7235) way to tell a client what scheme is expected; it
// reveals nothing about why this particular request was rejected.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// RequireAuth wraps next so it only runs once the request carries a
// valid, non-expired, non-revoked session belonging to an active user.
// Every rejection below — whatever its cause — produces the same
// unauthorized(w) response; only a genuine repository/database failure
// produces a 500, and even then the response body stays generic.
func (m *AuthMiddleware) RequireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := parseBearerToken(r.Header.Get("Authorization"))
		if !ok {
			unauthorized(w)
			return
		}

		session, err := m.sessionRepository.GetByTokenHash(r.Context(), hashSessionToken(token))
		if err != nil {
			if errors.Is(err, ErrSessionNotFound) {
				unauthorized(w)
				return
			}

			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !session.IsValid(time.Now().UTC()) {
			unauthorized(w)
			return
		}

		user, err := m.userRepository.GetByIDForAuthentication(r.Context(), session.UserID)
		if err != nil {
			if errors.Is(err, ErrUserNotFound) {
				unauthorized(w)
				return
			}

			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}

		if !user.IsActive {
			unauthorized(w)
			return
		}

		identity := AuthenticatedUser{
			UserID:         user.ID,
			OrganisationID: user.OrganisationID,
			Role:           user.Role,
		}

		next(w, r.WithContext(WithAuthenticatedUser(r.Context(), identity)))
	}
}

// parseBearerToken extracts the raw token from an Authorization header
// value, deliberately and conservatively: the header must consist of
// exactly two whitespace-separated fields — a scheme and a token — with
// the scheme case-insensitively "Bearer" (RFC 7235 permits case
// insensitive auth scheme names) and a non-empty token. Anything else —
// a missing header, the wrong scheme, no token, extra components, or a
// token split by internal whitespace — is rejected outright; no attempt
// is made to recover a token from malformed input.
func parseBearerToken(header string) (string, bool) {
	fields := strings.Fields(header)
	if len(fields) != 2 {
		return "", false
	}

	if !strings.EqualFold(fields[0], "Bearer") {
		return "", false
	}

	return fields[1], true
}
