package admin

import (
	"context"
	"errors"
	"fmt"
	"net/mail"
	"strings"

	"github.com/google/uuid"
)

// minPasswordLength is the smallest password UserService.Create accepts.
// No complexity rules (uppercase/number/symbol) are imposed beyond this —
// length is the dominant factor in password strength, and arbitrary
// composition rules are widely considered to push users toward weaker,
// more predictable passwords (e.g. "Password1!") rather than stronger
// ones. RegistrationService.Register enforces the same rule via
// normalizeAndValidateUserFields below, so the two entry points a new
// user's core fields can ever come through never drift apart.
const minPasswordLength = 12

var (
	ErrUserNameRequired     = errors.New("user name is required")
	ErrUserEmailRequired    = errors.New("user email is required")
	ErrUserEmailInvalid     = errors.New("user email is not a valid email address")
	ErrUserPasswordRequired = errors.New("user password is required")
	ErrUserPasswordTooShort = fmt.Errorf("user password must be at least %d characters", minPasswordLength)
	ErrUserRoleInvalid      = errors.New("user role is not valid")

	// ErrUserActorNotPermittedToCreateUsers is returned when the actor
	// creating a user is not an admin or manager — including a caller
	// with role UserRoleUser and a caller with any unrecognised role
	// string. An unrecognised actor role is deliberately not treated as
	// equivalent to UserRoleUser (which would happen to produce the same
	// rejection here, but not because it's implicitly downgraded to
	// "user" — it's rejected because it isn't admin or manager, full
	// stop). Maps to 403.
	ErrUserActorNotPermittedToCreateUsers = errors.New("actor is not permitted to create users")

	// ErrUserRoleAssignmentNotPermitted is returned when the actor is
	// permitted to create users at all (admin or manager) but has
	// requested a target role above their own privilege — specifically a
	// manager requesting anything other than UserRoleUser. Maps to 403.
	ErrUserRoleAssignmentNotPermitted = errors.New("actor is not permitted to assign this role")
)

// normalizeAndValidateUserFields trims and validates the three fields
// every new user must have, regardless of which of the two entry points
// created them: an existing organisation's admin/manager calling
// UserService.Create, or an anonymous caller calling
// RegistrationService.Register. Keeping this logic in one place is what
// keeps those two paths from silently drifting apart on a
// security-relevant rule like the password minimum length.
//
// Email format (Milestone 8 Part 2 section 20) is validated here via
// net/mail.ParseAddress — the same standard-library check
// OrganisationService.Update already uses for an organisation's email —
// because a user's email is an authentication identifier (the value
// AuthService.Login looks a user up by): an invalid address is a user
// who can never log in, and a syntactically-broken address is worth
// rejecting at creation rather than discovering later at login.
// CustomerService deliberately does not gain the equivalent check here —
// see CreateCustomerRequest's own doc comment for why a customer's email
// stays permissive/optional: it identifies a business contact, not a
// credential.
func normalizeAndValidateUserFields(name, email, password string) (normalizedName, normalizedEmail string, err error) {
	normalizedName = strings.TrimSpace(name)
	if normalizedName == "" {
		return "", "", ErrUserNameRequired
	}

	normalizedEmail = strings.ToLower(strings.TrimSpace(email))
	if normalizedEmail == "" {
		return "", "", ErrUserEmailRequired
	}

	if _, err := mail.ParseAddress(normalizedEmail); err != nil {
		return "", "", ErrUserEmailInvalid
	}

	if password == "" {
		return "", "", ErrUserPasswordRequired
	}

	if len(password) < minPasswordLength {
		return "", "", ErrUserPasswordTooShort
	}

	return normalizedName, normalizedEmail, nil
}

// validateTargetRole trims role, defaulting a blank value to UserRoleUser,
// and confirms it's one of the known roles. Shared by UserService.Create;
// RegistrationService.Register never calls this because a registration's
// first user is always UserRoleAdmin, hardcoded, never client-supplied.
func validateTargetRole(role string) (string, error) {
	role = strings.TrimSpace(role)
	if role == "" {
		role = UserRoleUser
	}

	switch role {
	case UserRoleAdmin, UserRoleManager, UserRoleUser:
		return role, nil
	default:
		return "", ErrUserRoleInvalid
	}
}

// CreateUserActor is who is performing a UserService.Create call — the
// authenticated caller's identity and role, not the new user being
// created. It exists so the service can enforce the privilege-escalation
// rule below using only what it actually needs (UserID for potential
// future audit use, Role for the rule itself), rather than depending on
// the whole HTTP-layer AuthenticatedUser type merely to read one field
// off it.
type CreateUserActor struct {
	UserID uuid.UUID
	Role   string
}

// UserService sits between the HTTP layer and the user repository. It
// depends on OrganisationRepository, not a concrete implementation,
// solely to check that the requesting organisation exists before a user
// is created against it — mirroring the same pattern InvoiceService uses
// for its customer/product existence checks.
type UserService struct {
	repository             UserRepository
	organisationRepository OrganisationRepository
}

func NewUserService(
	repository UserRepository,
	organisationRepository OrganisationRepository,
) *UserService {
	return &UserService{
		repository:             repository,
		organisationRepository: organisationRepository,
	}
}

// Create validates the request, enforces that actor may assign the
// requested target role, verifies the referenced organisation exists,
// hashes the password (the plaintext is never persisted or logged), and
// persists the new user.
//
// The privilege-escalation rule below is deliberately checked here — not
// only at the HTTP layer via RequireRole — so it holds regardless of how
// this method is ever called: through the HTTP handler, from a
// background job, from another transport, or directly from a test.
// Route-level role middleware is a coarse first gate ("must be admin or
// manager to reach this endpoint at all"); this is the unbypassable,
// fine-grained rule about which specific role that actor may assign to
// someone else. An admin may assign any already-validated role. A
// manager may only assign UserRoleUser. Every other actor role —
// including UserRoleUser and any unrecognised string — may assign
// nothing: the default branch below is what makes this fail closed for
// an unknown actor role without treating it as equivalent to a known,
// lesser-privileged one.
//
// email is normalised to lowercase after trimming — see
// normalizeAndValidateUserFields.
func (s *UserService) Create(
	ctx context.Context,
	organisationID uuid.UUID,
	actor CreateUserActor,
	request CreateUserRequest,
) (*User, error) {
	name, email, err := normalizeAndValidateUserFields(request.Name, request.Email, request.Password)
	if err != nil {
		return nil, err
	}

	role, err := validateTargetRole(request.Role)
	if err != nil {
		return nil, err
	}

	switch actor.Role {
	case UserRoleAdmin:
		// may assign any already-validated role
	case UserRoleManager:
		if role != UserRoleUser {
			return nil, ErrUserRoleAssignmentNotPermitted
		}
	default:
		return nil, ErrUserActorNotPermittedToCreateUsers
	}

	if _, err := s.organisationRepository.GetByID(ctx, organisationID); err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			return nil, err
		}

		return nil, fmt.Errorf("look up user organisation: %w", err)
	}

	passwordHash, err := hashPassword(request.Password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	user := &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           name,
		Email:          email,
		PasswordHash:   passwordHash,
		Role:           role,
		IsActive:       true,
	}

	if err := s.repository.Create(ctx, user); err != nil {
		return nil, err
	}

	return user, nil
}

// GetByID delegates to the repository; organisation scoping happens
// there.
func (s *UserService) GetByID(
	ctx context.Context,
	organisationID uuid.UUID,
	userID uuid.UUID,
) (*User, error) {
	return s.repository.GetByID(ctx, organisationID, userID)
}
