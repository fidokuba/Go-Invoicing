package admin

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
)

// middlewareTestFixture bundles an AuthMiddleware with in-memory fakes,
// reusing the same fakeSessionRepository/fakeUserRepository defined for
// AuthService's own tests.
type middlewareTestFixture struct {
	middleware        *AuthMiddleware
	sessionRepository *fakeSessionRepository
	userRepository    *fakeUserRepository
}

func newMiddlewareTestFixture() *middlewareTestFixture {
	sessions := newFakeSessionRepository()
	users := newFakeUserRepository()

	return &middlewareTestFixture{
		middleware:        NewAuthMiddleware(sessions, users),
		sessionRepository: sessions,
		userRepository:    users,
	}
}

// addUserWithSession seeds an active user and a valid session for the raw
// token, returning both so a test can mutate either before exercising the
// middleware (e.g. to expire or revoke the session, or deactivate the
// user).
func (f *middlewareTestFixture) addUserWithSession(rawToken string) (User, Session) {
	userID := uuid.New()
	user := User{
		ID:             userID,
		OrganisationID: uuid.New(),
		Name:           "Test User",
		Email:          "test-user@example.com",
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
	f.userRepository.usersByID[userID] = user
	f.userRepository.usersByEmail[user.Email] = user

	now := time.Now().UTC()
	session := Session{
		ID:        uuid.New(),
		UserID:    userID,
		TokenHash: hashSessionToken(rawToken),
		CreatedAt: now,
		ExpiresAt: now.Add(SessionTTL),
	}
	f.sessionRepository.sessions[session.ID] = session

	return user, session
}

// spyHandler is a downstream http.HandlerFunc that records whether it was
// called and captures whatever AuthenticatedUser it can see in the
// request's context.
type spyHandler struct {
	called   bool
	identity AuthenticatedUser
	hadOK    bool
}

func (s *spyHandler) handle(w http.ResponseWriter, r *http.Request) {
	s.called = true
	s.identity, s.hadOK = AuthenticatedUserFromContext(r.Context())
	w.WriteHeader(http.StatusOK)
}

func doAuthenticatedRequest(t *testing.T, middleware *AuthMiddleware, spy *spyHandler, authorizationHeader string) *httptest.ResponseRecorder {
	t.Helper()

	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authorizationHeader != "" {
		request.Header.Set("Authorization", authorizationHeader)
	}
	recorder := httptest.NewRecorder()

	middleware.RequireAuth(spy.handle)(recorder, request)

	return recorder
}

func TestAuthMiddleware_RequireAuth_ValidToken_CallsDownstreamHandler(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	f.addUserWithSession(rawToken)

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if !spy.called {
		t.Fatal("expected the downstream handler to be called")
	}
}

func TestAuthMiddleware_RequireAuth_DownstreamHandlerReceivesCorrectIdentity(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	user, session := f.addUserWithSession(rawToken)

	spy := &spyHandler{}
	doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if !spy.hadOK {
		t.Fatal("expected an AuthenticatedUser to be present in the downstream handler's context")
	}

	want := AuthenticatedUser{
		UserID:         user.ID,
		OrganisationID: user.OrganisationID,
		Role:           user.Role,
		SessionID:      session.ID,
	}

	if spy.identity != want {
		t.Errorf("expected identity %+v, got %+v", want, spy.identity)
	}
}

func TestAuthMiddleware_RequireAuth_MissingHeader(t *testing.T) {
	f := newMiddlewareTestFixture()
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_MalformedHeader(t *testing.T) {
	f := newMiddlewareTestFixture()

	malformedHeaders := []string{
		"Bearer",                        // scheme only, no token
		"sometoken",                     // no scheme at all
		"Bearer token1 token2",          // extra credential component
		"Bearer\ttoken-with-only-a-tab", // still just scheme+token; kept as a baseline alongside the next case
	}

	for _, header := range malformedHeaders {
		t.Run(header, func(t *testing.T) {
			spy := &spyHandler{}
			recorder := doAuthenticatedRequest(t, f.middleware, spy, header)

			// "Bearer\ttoken..." is actually well-formed (tab is whitespace,
			// splitting into exactly two fields) so it's expected to need a
			// valid session to succeed — since none exists, it still 401s,
			// just via the "unknown token" path rather than "malformed
			// header". Either way the downstream handler must not run.
			if recorder.Code != http.StatusUnauthorized {
				t.Errorf("header %q: expected status %d, got %d", header, http.StatusUnauthorized, recorder.Code)
			}

			if spy.called {
				t.Errorf("header %q: expected the downstream handler not to be called", header)
			}
		})
	}
}

func TestAuthMiddleware_RequireAuth_WrongScheme(t *testing.T) {
	f := newMiddlewareTestFixture()
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Basic dXNlcjpwYXNz")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_SchemeIsCaseInsensitive(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	f.addUserWithSession(rawToken)

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "bearer "+rawToken)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected a lowercase 'bearer' scheme to be accepted, got status %d (body: %s)", recorder.Code, recorder.Body.String())
	}

	if !spy.called {
		t.Error("expected the downstream handler to be called")
	}
}

func TestAuthMiddleware_RequireAuth_EmptyBearerToken(t *testing.T) {
	f := newMiddlewareTestFixture()
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer ")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_UnknownToken(t *testing.T) {
	f := newMiddlewareTestFixture()
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer no-such-token-was-ever-issued")

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

// TestAuthMiddleware_RequireAuth_RandomToken is UnknownToken's
// counterpart using an actually-generated random token rather than a
// hand-typed string, in case the two ever needed to diverge (e.g. one
// day testing something format-specific about generated tokens) — today
// they exercise the same code path and are expected to.
func TestAuthMiddleware_RequireAuth_RandomToken(t *testing.T) {
	f := newMiddlewareTestFixture()

	randomToken, err := generateSessionToken()
	if err != nil {
		t.Fatalf("generate random token: %v", err)
	}

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+randomToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

// TestAuthMiddleware_RequireAuth_OneCharacterModifiedToken proves there
// is no partial/prefix matching anywhere in the lookup path: a valid
// token with a single character changed hashes to something completely
// different (SHA-256 has no near-miss leniency) and is therefore rejected
// exactly like any other unknown token.
func TestAuthMiddleware_RequireAuth_OneCharacterModifiedToken(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	f.addUserWithSession(rawToken)

	modifiedToken := rawToken[:len(rawToken)-1] + "X"

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+modifiedToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

// TestAuthMiddleware_RequireAuth_DuplicateAuthorizationHeaders proves
// deterministic, fail-safe behaviour when a request carries two
// Authorization headers: net/http's Header.Get always returns only the
// first value for a given key, so only the first header is ever
// consulted here — a second header (valid or not) has no effect on the
// outcome either way.
func TestAuthMiddleware_RequireAuth_DuplicateAuthorizationHeaders(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	f.addUserWithSession(rawToken)

	spy := &spyHandler{}
	request := httptest.NewRequest(http.MethodGet, "/protected", nil)
	request.Header.Add("Authorization", "Bearer "+rawToken)
	request.Header.Add("Authorization", "Bearer some-other-token-entirely")
	recorder := httptest.NewRecorder()

	f.middleware.RequireAuth(spy.handle)(recorder, request)

	if recorder.Code != http.StatusOK {
		t.Fatalf("expected the first Authorization header to be used (status %d), got %d (body: %s)", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	if !spy.called {
		t.Error("expected the downstream handler to be called")
	}
}

func TestAuthMiddleware_RequireAuth_ExpiredSession(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "an-expired-session-token"
	_, session := f.addUserWithSession(rawToken)

	session.CreatedAt = time.Now().UTC().Add(-SessionTTL - time.Hour)
	session.ExpiresAt = session.CreatedAt.Add(SessionTTL) // already in the past
	f.sessionRepository.sessions[session.ID] = session

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_RevokedSession(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-revoked-session-token"
	_, session := f.addUserWithSession(rawToken)

	revokedAt := time.Now().UTC()
	session.RevokedAt = &revokedAt
	f.sessionRepository.sessions[session.ID] = session

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_InactiveUser(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "an-inactive-users-token"
	user, _ := f.addUserWithSession(rawToken)

	user.IsActive = false
	f.userRepository.usersByID[user.ID] = user
	f.userRepository.usersByEmail[user.Email] = user

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_DeletedOrNonexistentUser(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-token-for-a-user-that-no-longer-exists"
	user, _ := f.addUserWithSession(rawToken)

	// Simulate the user having been deleted: GetByIDForAuthentication (like
	// the real Postgres implementation's deleted_at IS NULL filter) simply
	// finds nothing for this ID any more.
	delete(f.userRepository.usersByID, user.ID)
	delete(f.userRepository.usersByEmail, user.Email)

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("expected status %d, got %d", http.StatusUnauthorized, recorder.Code)
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_SessionRepositoryFailure(t *testing.T) {
	f := newMiddlewareTestFixture()
	f.sessionRepository.getByTokenHashErr = errors.New("connection reset by peer")

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer whatever-token")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_UserRepositoryFailure(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-token-whose-user-lookup-fails"
	f.addUserWithSession(rawToken)
	f.userRepository.getByIDForAuthenticationErr = errors.New("connection reset by peer")

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d (body: %s)", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
	}

	if spy.called {
		t.Error("expected the downstream handler not to be called")
	}
}

func TestAuthMiddleware_RequireAuth_InternalServerErrorDoesNotExposeDetails(t *testing.T) {
	f := newMiddlewareTestFixture()
	f.sessionRepository.getByTokenHashErr = errors.New("pq: connection to server at \"10.0.0.5\" failed")

	spy := &spyHandler{}
	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer whatever-token")

	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("expected status %d, got %d", http.StatusInternalServerError, recorder.Code)
	}

	if got := recorder.Body.String(); got == "" || got == "pq: connection to server at \"10.0.0.5\" failed\n" {
		t.Errorf("expected a generic error body, got %q", got)
	}
}

// TestAuthMiddleware_RequireAuth_AllCredentialFailuresAreIndistinguishable
// is the crux security test: a missing header, a malformed header, the
// wrong scheme, an empty token, an unknown token, an expired session, a
// revoked session, an inactive user, and a deleted/nonexistent user must
// all produce exactly the same status and exactly the same body — so
// nothing about a rejected request leaks which of these actually
// happened.
func TestAuthMiddleware_RequireAuth_AllCredentialFailuresAreIndistinguishable(t *testing.T) {
	type scenario struct {
		name   string
		header string
		setup  func(f *middlewareTestFixture) string // returns the Authorization header to use, may depend on fixture state
	}

	scenarios := []scenario{
		{name: "missing header", setup: func(f *middlewareTestFixture) string { return "" }},
		{name: "malformed header", setup: func(f *middlewareTestFixture) string { return "not-a-valid-header-at-all" }},
		{name: "wrong scheme", setup: func(f *middlewareTestFixture) string { return "Basic dXNlcjpwYXNz" }},
		{name: "empty bearer token", setup: func(f *middlewareTestFixture) string { return "Bearer " }},
		{name: "unknown token", setup: func(f *middlewareTestFixture) string { return "Bearer no-such-token" }},
		{
			name: "expired session",
			setup: func(f *middlewareTestFixture) string {
				const rawToken = "expired-token-for-indistinguishability-test"
				_, session := f.addUserWithSession(rawToken)
				session.CreatedAt = time.Now().UTC().Add(-SessionTTL - time.Hour)
				session.ExpiresAt = session.CreatedAt.Add(SessionTTL)
				f.sessionRepository.sessions[session.ID] = session
				return "Bearer " + rawToken
			},
		},
		{
			name: "revoked session",
			setup: func(f *middlewareTestFixture) string {
				const rawToken = "revoked-token-for-indistinguishability-test"
				_, session := f.addUserWithSession(rawToken)
				revokedAt := time.Now().UTC()
				session.RevokedAt = &revokedAt
				f.sessionRepository.sessions[session.ID] = session
				return "Bearer " + rawToken
			},
		},
		{
			name: "inactive user",
			setup: func(f *middlewareTestFixture) string {
				const rawToken = "inactive-user-token-for-indistinguishability-test"
				user, _ := f.addUserWithSession(rawToken)
				user.IsActive = false
				f.userRepository.usersByID[user.ID] = user
				f.userRepository.usersByEmail[user.Email] = user
				return "Bearer " + rawToken
			},
		},
		{
			name: "deleted user",
			setup: func(f *middlewareTestFixture) string {
				const rawToken = "deleted-user-token-for-indistinguishability-test"
				user, _ := f.addUserWithSession(rawToken)
				delete(f.userRepository.usersByID, user.ID)
				delete(f.userRepository.usersByEmail, user.Email)
				return "Bearer " + rawToken
			},
		},
	}

	var referenceStatus int
	var referenceBody string

	for _, s := range scenarios {
		t.Run(s.name, func(t *testing.T) {
			f := newMiddlewareTestFixture()
			header := s.setup(f)

			spy := &spyHandler{}
			recorder := doAuthenticatedRequest(t, f.middleware, spy, header)

			if recorder.Code != http.StatusUnauthorized {
				t.Fatalf("expected status %d, got %d (body: %s)", http.StatusUnauthorized, recorder.Code, recorder.Body.String())
			}

			if referenceStatus == 0 {
				referenceStatus = recorder.Code
				referenceBody = recorder.Body.String()
				return
			}

			if recorder.Code != referenceStatus || recorder.Body.String() != referenceBody {
				t.Errorf(
					"expected the same response as the other scenarios (status %d, body %q), got status %d, body %q",
					referenceStatus, referenceBody, recorder.Code, recorder.Body.String(),
				)
			}
		})
	}
}

func TestAuthMiddleware_RequireAuth_UnauthorizedResponsesIncludeWWWAuthenticateHeader(t *testing.T) {
	f := newMiddlewareTestFixture()
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "")

	if got := recorder.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Errorf("expected WWW-Authenticate: Bearer, got %q", got)
	}
}

func TestAuthMiddleware_RequireAuth_InternalServerErrorDoesNotIncludeWWWAuthenticateHeader(t *testing.T) {
	// A 500 is not an authentication challenge — WWW-Authenticate belongs
	// only on the 401 responses.
	f := newMiddlewareTestFixture()
	f.sessionRepository.getByTokenHashErr = errors.New("connection reset by peer")
	spy := &spyHandler{}

	recorder := doAuthenticatedRequest(t, f.middleware, spy, "Bearer whatever-token")

	if got := recorder.Header().Get("WWW-Authenticate"); got != "" {
		t.Errorf("expected no WWW-Authenticate header on a 500, got %q", got)
	}
}

// TestAuthMiddleware_RequireAuth_TokenIsHashedBeforeLookup proves the raw
// bearer token is never itself passed to the session repository — only
// its SHA-256 hash is, exactly as session_repository.go's contract
// requires.
func TestAuthMiddleware_RequireAuth_TokenIsHashedBeforeLookup(t *testing.T) {
	f := newMiddlewareTestFixture()
	const rawToken = "a-valid-raw-session-token"
	f.addUserWithSession(rawToken)

	spy := &spyHandler{}
	doAuthenticatedRequest(t, f.middleware, spy, "Bearer "+rawToken)

	if f.sessionRepository.lastTokenHashArg != hashSessionToken(rawToken) {
		t.Errorf("expected GetByTokenHash to be called with the token's hash, got %q", f.sessionRepository.lastTokenHashArg)
	}

	if f.sessionRepository.lastTokenHashArg == rawToken {
		t.Error("expected GetByTokenHash to never be called with the raw token")
	}
}
