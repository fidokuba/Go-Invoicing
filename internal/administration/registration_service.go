package admin

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
)

// RegistrationResult is what a successful Register returns: the newly
// created organisation and its first user. There is no session/token
// here — registration does not authenticate the caller; see
// RegistrationService.Register's doc comment.
type RegistrationResult struct {
	Organisation *Organisation
	User         *User
}

// RegistrationService is the only way a new organisation and its first
// (admin) user may ever be created — the two-step public bootstrap
// (public POST /organisations, then public POST /users?organisationId=)
// it replaces is removed entirely (Milestone 4 Part 5).
//
// It is deliberately a separate type from OrganisationService and
// UserService, not a method added to either: registration's atomicity
// requirement (organisation + settings + user succeed or fail together)
// and its distinct rule ("the first user's role is always admin, never
// client-supplied") don't belong on either of those services, which have
// their own, different jobs and are called from authenticated contexts
// this one deliberately isn't.
type RegistrationService struct {
	organisationRepository OrganisationRepository
	settingsRepository     SettingsRepository
	userRepository         UserRepository
	txBeginner             TxBeginner
}

func NewRegistrationService(
	organisationRepository OrganisationRepository,
	settingsRepository SettingsRepository,
	userRepository UserRepository,
	txBeginner TxBeginner,
) *RegistrationService {
	return &RegistrationService{
		organisationRepository: organisationRepository,
		settingsRepository:     settingsRepository,
		userRepository:         userRepository,
		txBeginner:             txBeginner,
	}
}

// Register validates the organisation name and the first user's fields,
// then atomically creates the organisation, its default settings (the
// same defaults OrganisationService.Create uses today — see below), and
// the first user with role hardcoded to UserRoleAdmin — never read from
// request input, so an anonymous caller cannot request any other initial
// role.
//
// BEGIN/COMMIT below is a single transaction across all three writes:
// if any of them fails, everything is rolled back, so a failed
// registration never leaves an orphan organisation, an orphan settings
// row, or a partially-created user. This closes a gap the old
// OrganisationService.Create explicitly accepted (organisation and
// settings were two separate, non-atomic writes) as well as bootstrap's
// original problem (an organisation could exist with no user able to log
// into it).
//
// Register does not create a session and does not authenticate the
// caller — it only creates rows. The client must call AuthService.Login
// afterward to obtain a bearer token, the same as any other user.
func (s *RegistrationService) Register(
	ctx context.Context,
	organisationName string,
	userName string,
	userEmail string,
	userPassword string,
) (*RegistrationResult, error) {
	organisationName = strings.TrimSpace(organisationName)
	if organisationName == "" {
		return nil, ErrOrganisationNameRequired
	}

	name, email, err := normalizeAndValidateUserFields(userName, userEmail, userPassword)
	if err != nil {
		return nil, err
	}

	passwordHash, err := hashPassword(userPassword)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	organisation := &Organisation{
		ID:   uuid.New(),
		Name: organisationName,
	}

	// Same defaults OrganisationService.Create uses: InvoiceNumber starts
	// at 0 ("nothing allocated yet") so the first invoice this
	// organisation ever creates gets number 1 via
	// Settings.NextInvoiceNumber.
	settings := &Settings{
		ID:             uuid.New(),
		OrganisationID: organisation.ID,
		InvoicePrefix:  "INV-",
		InvoiceNumber:  0,
		Currency:       "GBP",
		PaymentTerms:   30,
	}

	user := &User{
		ID:             uuid.New(),
		OrganisationID: organisation.ID,
		Name:           name,
		Email:          email,
		PasswordHash:   passwordHash,
		Role:           UserRoleAdmin,
		IsActive:       true,
	}

	// BEGIN — the organisation, its settings, and its first user either
	// all succeed together or are all undone together.
	tx, err := s.txBeginner.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin registration transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if err := s.organisationRepository.WithTx(tx).Create(ctx, organisation); err != nil {
		return nil, fmt.Errorf("create organisation: %w", err)
	}

	if err := s.settingsRepository.WithTx(tx).Create(ctx, settings); err != nil {
		return nil, fmt.Errorf("create organisation settings: %w", err)
	}

	if err := s.userRepository.WithTx(tx).Create(ctx, user); err != nil {
		if errors.Is(err, ErrUserEmailAlreadyExists) {
			return nil, err
		}

		return nil, fmt.Errorf("create first user: %w", err)
	}

	// COMMIT — only reached once all three writes above succeeded.
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit registration transaction: %w", err)
	}

	return &RegistrationResult{Organisation: organisation, User: user}, nil
}
