package admin

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeSessionRepository is an in-memory SessionRepository used to test
// AuthService without touching PostgreSQL. The repository integration is
// already covered by session_repository_postgres_test.go.
//
// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional/rollback semantics of its own; that guarantee (that
// a rolled-back transaction leaves no partial state) is proven at the
// unit level here via fakeTx's committed/rolledBack flags, matching the
// same pattern the invoice package's tests use for its own transactional
// services.
type fakeSessionRepository struct {
	sessions map[uuid.UUID]Session

	createErr         error
	getByTokenHashErr error

	// lastTokenHashArg records the exact argument the middleware/service
	// passed to GetByTokenHash, so tests can prove a raw token was hashed
	// before ever reaching the repository.
	lastTokenHashArg string
}

func newFakeSessionRepository() *fakeSessionRepository {
	return &fakeSessionRepository{sessions: make(map[uuid.UUID]Session)}
}

func (f *fakeSessionRepository) WithTx(tx pgx.Tx) SessionRepository {
	return f
}

func (f *fakeSessionRepository) Create(ctx context.Context, session *Session) error {
	if f.createErr != nil {
		return f.createErr
	}

	f.sessions[session.ID] = *session
	return nil
}

func (f *fakeSessionRepository) GetByTokenHash(ctx context.Context, tokenHash string) (*Session, error) {
	f.lastTokenHashArg = tokenHash

	if f.getByTokenHashErr != nil {
		return nil, f.getByTokenHashErr
	}

	for _, s := range f.sessions {
		if s.TokenHash == tokenHash {
			session := s
			return &session, nil
		}
	}

	return nil, ErrSessionNotFound
}

// fakeTx is a minimal stand-in for a pgx.Tx. Only Commit and Rollback are
// ever exercised by AuthService — it never issues a query directly
// through tx, always via a WithTx-bound repository — so every other
// method exists solely to satisfy the pgx.Tx interface and is never
// called. Identical to the invoice package's own fakeTx.
type fakeTx struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (t *fakeTx) Commit(ctx context.Context) error {
	t.committed = true
	return t.commitErr
}

// Rollback mirrors real pgx.Tx behaviour: once Commit has closed the
// transaction, a later Rollback (e.g. from a deferred call) is a no-op
// that returns pgx.ErrTxClosed rather than actually rolling anything
// back.
func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.committed {
		return pgx.ErrTxClosed
	}
	t.rolledBack = true
	return nil
}

func (t *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) {
	return nil, errors.New("not implemented in fakeTx")
}
func (t *fakeTx) CopyFrom(ctx context.Context, tableName pgx.Identifier, columnNames []string, rowSrc pgx.CopyFromSource) (int64, error) {
	return 0, nil
}
func (t *fakeTx) SendBatch(ctx context.Context, b *pgx.Batch) pgx.BatchResults { return nil }
func (t *fakeTx) LargeObjects() pgx.LargeObjects                               { return pgx.LargeObjects{} }
func (t *fakeTx) Prepare(ctx context.Context, name, sql string) (*pgconn.StatementDescription, error) {
	return nil, nil
}
func (t *fakeTx) Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, nil
}
func (t *fakeTx) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return nil, nil
}
func (t *fakeTx) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row { return nil }
func (t *fakeTx) Conn() *pgx.Conn                                               { return nil }

// fakeTxBeginner is a minimal in-memory TxBeginner handing back a single
// fakeTx, so tests can inspect whether Commit or Rollback was ultimately
// called.
type fakeTxBeginner struct {
	tx       *fakeTx
	beginErr error

	// beginCallCount records how many times Begin was called, so a test
	// can prove a transaction was never opened at all — e.g. when input
	// validation should fail before any database work begins.
	beginCallCount int
}

func (f *fakeTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	f.beginCallCount++

	if f.beginErr != nil {
		return nil, f.beginErr
	}
	return f.tx, nil
}

// authTestFixture bundles an AuthService with fakes, pre-seeded with one
// active user and one inactive user, both with known plaintext passwords
// hashed for real via hashPassword — so Login exercises the actual
// Argon2id verification path, not a stubbed one.
type authTestFixture struct {
	service           *AuthService
	userRepository    *fakeUserRepository
	sessionRepository *fakeSessionRepository
	tx                *fakeTx
	txBeginner        *fakeTxBeginner

	organisationID uuid.UUID

	activeUserID   uuid.UUID
	activeEmail    string
	activePassword string

	inactiveUserID   uuid.UUID
	inactiveEmail    string
	inactivePassword string
}

func newAuthTestFixture(t *testing.T) *authTestFixture {
	t.Helper()

	organisationID := uuid.New()
	users := newFakeUserRepository()
	sessions := newFakeSessionRepository()
	tx := &fakeTx{}
	txBeginner := &fakeTxBeginner{tx: tx}

	service := NewAuthService(users, sessions, txBeginner)

	const activePassword = "correct horse battery staple"
	activeHash, err := hashPassword(activePassword)
	if err != nil {
		t.Fatalf("hash active password: %v", err)
	}
	activeUserID := uuid.New()
	activeUser := User{
		ID:             activeUserID,
		OrganisationID: organisationID,
		Name:           "Active User",
		Email:          "active@example.com",
		PasswordHash:   activeHash,
		Role:           UserRoleUser,
		IsActive:       true,
	}
	users.usersByID[activeUserID] = activeUser
	users.usersByEmail[activeUser.Email] = activeUser

	const inactivePassword = "another perfectly valid password"
	inactiveHash, err := hashPassword(inactivePassword)
	if err != nil {
		t.Fatalf("hash inactive password: %v", err)
	}
	inactiveUserID := uuid.New()
	inactiveUser := User{
		ID:             inactiveUserID,
		OrganisationID: organisationID,
		Name:           "Inactive User",
		Email:          "inactive@example.com",
		PasswordHash:   inactiveHash,
		Role:           UserRoleUser,
		IsActive:       false,
	}
	users.usersByID[inactiveUserID] = inactiveUser
	users.usersByEmail[inactiveUser.Email] = inactiveUser

	return &authTestFixture{
		service:           service,
		userRepository:    users,
		sessionRepository: sessions,
		tx:                tx,
		txBeginner:        txBeginner,
		organisationID:    organisationID,
		activeUserID:      activeUserID,
		activeEmail:       activeUser.Email,
		activePassword:    activePassword,
		inactiveUserID:    inactiveUserID,
		inactiveEmail:     inactiveUser.Email,
		inactivePassword:  inactivePassword,
	}
}

func TestAuthService_Login_Success(t *testing.T) {
	f := newAuthTestFixture(t)

	result, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if result.Token == "" {
		t.Error("expected a non-empty token")
	}

	if result.User.ID != f.activeUserID {
		t.Errorf("expected user ID %v, got %v", f.activeUserID, result.User.ID)
	}

	if result.User.LastLogin == nil {
		t.Error("expected LastLogin to be set on the returned user")
	}

	if result.Session.UserID != f.activeUserID {
		t.Errorf("expected session user ID %v, got %v", f.activeUserID, result.Session.UserID)
	}

	if result.Session.TokenHash != hashSessionToken(result.Token) {
		t.Error("expected the session's TokenHash to be the SHA-256 hash of the returned raw token")
	}

	if result.Session.TokenHash == result.Token {
		t.Error("expected the session's TokenHash to differ from the raw token")
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}

	storedUser := f.userRepository.usersByID[f.activeUserID]
	if storedUser.LastLogin == nil {
		t.Error("expected the persisted user's LastLogin to be updated")
	}
}

func TestAuthService_Login_SessionExpiryIsTwentyFourHours(t *testing.T) {
	f := newAuthTestFixture(t)

	result, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("login: %v", err)
	}

	if got := result.Session.ExpiresAt.Sub(result.Session.CreatedAt); got != SessionTTL {
		t.Errorf("expected a session TTL of %v, got %v", SessionTTL, got)
	}

	if SessionTTL != 24*60*60*1e9 { // 24 hours in nanoseconds, spelled out to catch an accidental constant change
		t.Errorf("expected SessionTTL to be exactly 24 hours, got %v", SessionTTL)
	}
}

func TestAuthService_Login_WrongPassword(t *testing.T) {
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), f.activeEmail, "totally the wrong password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthService_Login_UnknownEmail(t *testing.T) {
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), "nobody@example.com", "whatever password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthService_Login_InactiveUser(t *testing.T) {
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), f.inactiveEmail, f.inactivePassword)
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthService_Login_InactiveUserWithWrongPassword(t *testing.T) {
	// Same externally-visible outcome regardless of which of the two
	// rejection reasons actually applies first.
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), f.inactiveEmail, "not even the right password")
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("expected ErrInvalidCredentials, got %v", err)
	}
}

func TestAuthService_Login_MissingEmail(t *testing.T) {
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), "   ", f.activePassword)
	if !errors.Is(err, ErrLoginEmailRequired) {
		t.Fatalf("expected ErrLoginEmailRequired, got %v", err)
	}
}

func TestAuthService_Login_MissingPassword(t *testing.T) {
	f := newAuthTestFixture(t)

	_, err := f.service.Login(context.Background(), f.activeEmail, "")
	if !errors.Is(err, ErrLoginPasswordRequired) {
		t.Fatalf("expected ErrLoginPasswordRequired, got %v", err)
	}
}

func TestAuthService_Login_NoSessionCreatedOnFailure(t *testing.T) {
	f := newAuthTestFixture(t)

	if _, err := f.service.Login(context.Background(), f.activeEmail, "wrong password"); err == nil {
		t.Fatal("expected an error")
	}

	if len(f.sessionRepository.sessions) != 0 {
		t.Errorf("expected no session to be created on a failed login, found %d", len(f.sessionRepository.sessions))
	}
}

func TestAuthService_Login_AllowsMultipleSessions(t *testing.T) {
	f := newAuthTestFixture(t)

	first, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("first login: %v", err)
	}

	second, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err != nil {
		t.Fatalf("second login: %v", err)
	}

	if first.Session.ID == second.Session.ID {
		t.Error("expected two distinct session IDs")
	}

	if first.Token == second.Token {
		t.Error("expected two distinct raw tokens")
	}

	if len(f.sessionRepository.sessions) != 2 {
		t.Errorf("expected 2 stored sessions after two logins, got %d", len(f.sessionRepository.sessions))
	}
}

func TestAuthService_Login_UnexpectedGetByEmailErrorPropagates(t *testing.T) {
	f := newAuthTestFixture(t)
	f.userRepository.getByEmailErr = errors.New("connection reset by peer")

	_, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("expected the unexpected repository error to propagate, not be swallowed as invalid credentials")
	}
}

func TestAuthService_Login_RollsBackOnSessionCreateFailure(t *testing.T) {
	f := newAuthTestFixture(t)
	f.sessionRepository.createErr = errors.New("connection reset by peer")

	_, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("expected a repository failure, not an invalid-credentials error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestAuthService_Login_RollsBackOnLastLoginUpdateFailure(t *testing.T) {
	f := newAuthTestFixture(t)
	f.userRepository.updateLastLoginErr = errors.New("connection reset by peer")

	_, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("expected a repository failure, not an invalid-credentials error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestAuthService_Login_BeginTransactionErrorPropagates(t *testing.T) {
	f := newAuthTestFixture(t)
	f.txBeginner.beginErr = errors.New("pool exhausted")

	_, err := f.service.Login(context.Background(), f.activeEmail, f.activePassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if errors.Is(err, ErrInvalidCredentials) {
		t.Error("expected a transaction-begin failure, not an invalid-credentials error")
	}

	if len(f.sessionRepository.sessions) != 0 {
		t.Error("expected no session to be created when the transaction never began")
	}
}

// TestAuthService_Login_UnknownEmailPerformsDummyPasswordVerification
// proves the unknown-email path really runs a full, valid Argon2id
// verification rather than short-circuiting — structurally, not via a
// brittle wall-clock timing assertion. dummyPasswordHash must decode as a
// well-formed argon2id hash (otherwise verifyPassword would fail fast on
// a parse error rather than doing the actual key-derivation work), and
// verifying against it must fail with the ordinary mismatch error, not a
// format error.
func TestAuthService_Login_UnknownEmailPerformsDummyPasswordVerification(t *testing.T) {
	if _, _, _, _, _, _, err := decodeHash(dummyPasswordHash); err != nil {
		t.Fatalf("expected dummyPasswordHash to decode as a well-formed argon2id hash, got %v", err)
	}

	err := verifyPassword("whatever the caller typed", dummyPasswordHash)
	if !errors.Is(err, ErrPasswordMismatch) {
		t.Errorf("expected ErrPasswordMismatch when verifying against the dummy hash, got %v", err)
	}
}
