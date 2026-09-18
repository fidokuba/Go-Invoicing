package admin

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// registrationTestFixture bundles a RegistrationService with the same
// fakes AuthService's own tests use (fakeOrganisationRepository,
// fakeSettingsRepository, fakeUserRepository, fakeTx/fakeTxBeginner),
// proving atomicity through the shared tx.committed/rolledBack flags
// exactly like AuthService.Login's own rollback tests.
type registrationTestFixture struct {
	service                *RegistrationService
	organisationRepository *fakeOrganisationRepository
	settingsRepository     *fakeSettingsRepository
	userRepository         *fakeUserRepository
	tx                     *fakeTx
	txBeginner             *fakeTxBeginner
}

func newRegistrationTestFixture() *registrationTestFixture {
	organisations := newFakeOrganisationRepository()
	settings := newFakeSettingsRepository()
	users := newFakeUserRepository()
	tx := &fakeTx{}
	txBeginner := &fakeTxBeginner{tx: tx}

	service := NewRegistrationService(organisations, settings, users, txBeginner)

	return &registrationTestFixture{
		service:                service,
		organisationRepository: organisations,
		settingsRepository:     settings,
		userRepository:         users,
		tx:                     tx,
		txBeginner:             txBeginner,
	}
}

const registrationPassword = "correct horse battery staple"

func TestRegistrationService_Register_Success(t *testing.T) {
	f := newRegistrationTestFixture()

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "Alice@Example.com", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if result.Organisation.Name != "Acme Ltd" {
		t.Errorf("expected organisation name %q, got %q", "Acme Ltd", result.Organisation.Name)
	}

	if result.User.OrganisationID != result.Organisation.ID {
		t.Errorf("expected user's OrganisationID %v to match the created organisation %v", result.User.OrganisationID, result.Organisation.ID)
	}

	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}

	if f.tx.rolledBack {
		t.Error("expected the transaction not to be rolled back")
	}
}

func TestRegistrationService_Register_CreatesDefaultSettings(t *testing.T) {
	f := newRegistrationTestFixture()

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	settings, ok := f.settingsRepository.settings[result.Organisation.ID]
	if !ok {
		t.Fatal("expected a settings row to have been created for the new organisation")
	}

	if settings.InvoicePrefix != "INV-" || settings.InvoiceNumber != 0 || settings.Currency != "GBP" || settings.PaymentTerms != 30 {
		t.Errorf("expected the same defaults OrganisationService.Create uses, got %+v", settings)
	}
}

// TestRegistrationService_Register_FirstUserIsAlwaysAdmin proves the
// created user's role is always UserRoleAdmin — RegisterUserRequest (and
// therefore Register's parameters) has no role input at all, so there is
// no way for an anonymous caller to request anything else.
func TestRegistrationService_Register_FirstUserIsAlwaysAdmin(t *testing.T) {
	f := newRegistrationTestFixture()

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if result.User.Role != UserRoleAdmin {
		t.Errorf("expected the first user's role to be %q, got %q", UserRoleAdmin, result.User.Role)
	}
}

func TestRegistrationService_Register_NormalizesEmail(t *testing.T) {
	f := newRegistrationTestFixture()

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "  Alice@Example.COM  ", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if result.User.Email != "alice@example.com" {
		t.Errorf("expected normalised email %q, got %q", "alice@example.com", result.User.Email)
	}
}

func TestRegistrationService_Register_PasswordStoredOnlyAsArgon2idHash(t *testing.T) {
	f := newRegistrationTestFixture()

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err != nil {
		t.Fatalf("register: %v", err)
	}

	if result.User.PasswordHash == registrationPassword {
		t.Fatal("expected PasswordHash to be a hash, not the plaintext password")
	}

	if strings.Contains(result.User.PasswordHash, registrationPassword) {
		t.Fatal("expected PasswordHash not to contain the plaintext password")
	}

	if !strings.HasPrefix(result.User.PasswordHash, "$argon2id$") {
		t.Errorf("expected an argon2id-formatted hash, got %q", result.User.PasswordHash)
	}

	if err := verifyPassword(registrationPassword, result.User.PasswordHash); err != nil {
		t.Errorf("expected the stored hash to verify against the original password, got %v", err)
	}
}

func TestRegistrationService_Register_MissingOrganisationName(t *testing.T) {
	f := newRegistrationTestFixture()

	_, err := f.service.Register(context.Background(), "   ", "Alice", "alice@example.com", registrationPassword)
	if !errors.Is(err, ErrOrganisationNameRequired) {
		t.Fatalf("expected ErrOrganisationNameRequired, got %v", err)
	}
}

func TestRegistrationService_Register_MissingUserName(t *testing.T) {
	f := newRegistrationTestFixture()

	_, err := f.service.Register(context.Background(), "Acme Ltd", "   ", "alice@example.com", registrationPassword)
	if !errors.Is(err, ErrUserNameRequired) {
		t.Fatalf("expected ErrUserNameRequired, got %v", err)
	}
}

func TestRegistrationService_Register_PasswordTooShort(t *testing.T) {
	f := newRegistrationTestFixture()

	_, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", "short")
	if !errors.Is(err, ErrUserPasswordTooShort) {
		t.Fatalf("expected ErrUserPasswordTooShort, got %v", err)
	}
}

// TestRegistrationService_Register_ValidationFailureBeginsNoTransaction
// proves input validation runs entirely before Begin is ever called —
// there is no reason to open a database transaction for a request that's
// already known to be invalid.
func TestRegistrationService_Register_ValidationFailureBeginsNoTransaction(t *testing.T) {
	f := newRegistrationTestFixture()

	if _, err := f.service.Register(context.Background(), "", "Alice", "alice@example.com", registrationPassword); err == nil {
		t.Fatal("expected an error")
	}

	if f.txBeginner.beginCallCount != 0 {
		t.Errorf("expected Begin to never be called for a validation failure, got %d calls", f.txBeginner.beginCallCount)
	}
}

func TestRegistrationService_Register_DuplicateEmailFails(t *testing.T) {
	f := newRegistrationTestFixture()

	if _, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword); err != nil {
		t.Fatalf("first registration: %v", err)
	}

	_, err := f.service.Register(context.Background(), "Second Co", "Alice Again", "alice@example.com", registrationPassword)
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists, got %v", err)
	}
}

func TestRegistrationService_Register_RollsBackOnOrganisationCreateFailure(t *testing.T) {
	f := newRegistrationTestFixture()
	f.organisationRepository.createErr = errors.New("connection reset by peer")

	_, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	if len(f.settingsRepository.settings) != 0 {
		t.Error("expected no settings to have been created")
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

func TestRegistrationService_Register_RollsBackOnSettingsCreateFailure(t *testing.T) {
	f := newRegistrationTestFixture()
	f.settingsRepository.createErr = errors.New("connection reset by peer")

	_, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

func TestRegistrationService_Register_RollsBackOnUserCreateFailure(t *testing.T) {
	f := newRegistrationTestFixture()
	f.userRepository.createErr = errors.New("connection reset by peer")

	_, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}

	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestRegistrationService_Register_CommitFailureDoesNotReportSuccess(t *testing.T) {
	f := newRegistrationTestFixture()
	f.tx.commitErr = errors.New("connection reset by peer")

	result, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err == nil {
		t.Fatal("expected an error when commit fails")
	}

	if result != nil {
		t.Errorf("expected a nil result on commit failure, got %+v", result)
	}
}

func TestRegistrationService_Register_BeginTransactionErrorPropagates(t *testing.T) {
	f := newRegistrationTestFixture()
	f.txBeginner.beginErr = errors.New("pool exhausted")

	_, err := f.service.Register(context.Background(), "Acme Ltd", "Alice", "alice@example.com", registrationPassword)
	if err == nil {
		t.Fatal("expected an error")
	}

	if len(f.organisationRepository.organisations) != 0 {
		t.Error("expected no organisation to have been created when the transaction never began")
	}
}
