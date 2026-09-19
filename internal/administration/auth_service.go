package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// ErrInvalidCredentials is returned by Login for every externally
// indistinguishable authentication failure: unknown email, wrong
// password, and inactive account. AuthHandler maps this — and only
// this — to a generic 401; it never learns which of the three actually
// happened.
var ErrInvalidCredentials = errors.New("invalid email or password")

var (
	ErrLoginEmailRequired    = errors.New("login email is required")
	ErrLoginPasswordRequired = errors.New("login password is required")
)

// dummyPasswordHash is a validly-formatted Argon2id hash with no
// corresponding real password anyone could know. Login verifies against
// it whenever no user is found for the supplied email, so the
// unknown-email path performs the same Argon2id work a real login would.
// Without this, an unknown email would fail immediately after a single
// database lookup — a timing difference an attacker could use to learn
// which emails are registered, i.e. account enumeration.
const dummyPasswordHash = "$argon2id$v=19$m=19456,t=2,p=1$AAAAAAAAAAAAAAAAAAAAAA$AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"

// TxBeginner starts a new transaction. *pgxpool.Pool satisfies this
// directly. Same pattern, and the same rationale, as invoice.TxBeginner:
// it's what lets AuthService itself own Begin/Commit/Rollback for the
// session-creation-plus-LastLogin-update unit, rather than either
// repository.
type TxBeginner interface {
	Begin(ctx context.Context) (pgx.Tx, error)
}

// LoginResult is what a successful Login returns: the raw session token
// (never persisted — see Session.TokenHash), the persisted Session
// record, and the authenticated User.
type LoginResult struct {
	Token   string
	Session *Session
	User    *User
}

// AuthService handles authentication — verifying who a caller is — kept
// deliberately separate from UserService, which handles user management
// (creating and reading user records). Login has a different failure
// model than anything on UserService (every failure must look identical
// externally) and different dependencies (SessionRepository, password
// verification, a transactional LastLogin update) that don't belong on
// the type responsible for user CRUD.
type AuthService struct {
	userRepository    UserRepository
	sessionRepository SessionRepository
	txBeginner        TxBeginner
}

func NewAuthService(
	userRepository UserRepository,
	sessionRepository SessionRepository,
	txBeginner TxBeginner,
) *AuthService {
	return &AuthService{
		userRepository:    userRepository,
		sessionRepository: sessionRepository,
		txBeginner:        txBeginner,
	}
}

// Login verifies an email/password pair and, on success, creates a new
// session and records the login time. It returns ErrInvalidCredentials —
// and only that — for every case a caller must not be able to
// distinguish: no user with that email, a wrong password, and a
// deactivated account. See dummyPasswordHash for why the unknown-email
// path still performs a full Argon2id verification. Any other repository
// failure is returned unwrapped-of-its-distinction (i.e. not folded into
// ErrInvalidCredentials), so a genuine infrastructure failure is never
// misreported as a bad password.
//
// Session creation and the User.LastLogin update happen inside a single
// database transaction (the BEGIN/COMMIT markers below): a login is
// either fully recorded — a queryable session AND an updated LastLogin —
// or not recorded at all. Without this, a failure between the two writes
// could hand a client a working session token for a user whose LastLogin
// was never updated, an inconsistency with no clean way to detect or
// repair after the fact.
func (s *AuthService) Login(ctx context.Context, email, password string) (*LoginResult, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	if email == "" {
		return nil, ErrLoginEmailRequired
	}

	if password == "" {
		return nil, ErrLoginPasswordRequired
	}

	user, err := s.userRepository.GetByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, ErrUserNotFound) {
			// Dummy verification: see dummyPasswordHash's comment. Its
			// result is always a mismatch, and is deliberately discarded.
			_ = verifyPassword(password, dummyPasswordHash)
			return nil, ErrInvalidCredentials
		}

		return nil, fmt.Errorf("look up user for login: %w", err)
	}

	// Any verification failure — wrong password or even a malformed
	// stored hash — is treated as invalid credentials. A malformed hash
	// would be an internal data problem, but surfacing that distinction
	// to the caller would leak exactly the kind of account-state signal
	// this method exists to hide.
	if err := verifyPassword(password, user.PasswordHash); err != nil {
		return nil, ErrInvalidCredentials
	}

	if !user.IsActive {
		return nil, ErrInvalidCredentials
	}

	rawToken, err := generateSessionToken()
	if err != nil {
		return nil, fmt.Errorf("generate session token: %w", err)
	}

	loggedInAt := time.Now().UTC()

	session := &Session{
		ID:        uuid.New(),
		UserID:    user.ID,
		TokenHash: hashSessionToken(rawToken),
		CreatedAt: loggedInAt,
		ExpiresAt: loggedInAt.Add(SessionTTL),
	}

	// BEGIN — the session INSERT and the LastLogin UPDATE either both
	// succeed or are both undone together.
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin login transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.sessionRepository.WithTx(tx).Create(ctx, session); err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	if err := s.userRepository.WithTx(tx).UpdateLastLogin(ctx, user.ID, loggedInAt); err != nil {
		return nil, fmt.Errorf("update last login: %w", err)
	}

	// COMMIT — only reached once both writes above succeeded.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit login transaction: %w", err)
	}

	user.LastLogin = &loggedInAt

	return &LoginResult{
		Token:   rawToken,
		Session: session,
		User:    user,
	}, nil
}

// Logout (Milestone 8 Part 3) revokes exactly one session, by its ID —
// never by re-deriving identity from a token or user ID, so it can only
// ever revoke the single session AuthMiddleware already resolved for
// this request (AuthenticatedUser.SessionID), not every session
// belonging to that user. No transaction is needed: this is a single
// UPDATE with no other write to keep in step with it.
func (s *AuthService) Logout(ctx context.Context, sessionID uuid.UUID) error {
	return s.sessionRepository.Revoke(ctx, sessionID)
}
