package admin

import (
	"context"
	"errors"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// fakeUserRepository is an in-memory UserRepository used to test the
// service/handler without touching PostgreSQL. The repository integration
// is already covered by user_repository_postgres_test.go.
//
// usersByEmail is keyed by email alone, not by organisation: email
// uniqueness is global (Milestone 4 Part 2), so this fake's duplicate
// check must reject a repeated email regardless of which organisation the
// second attempt targets, exactly like the real users_email_unique
// constraint.
//
// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional/rollback semantics of its own, matching the same
// caveat noted on fakeSettingsRepository/fakeInvoiceRepository in the
// invoice package's tests.
type fakeUserRepository struct {
	usersByID    map[uuid.UUID]User
	usersByEmail map[string]User

	createErr                   error
	getByEmailErr               error
	updateLastLoginErr          error
	getByIDForAuthenticationErr error
}

func newFakeUserRepository() *fakeUserRepository {
	return &fakeUserRepository{
		usersByID:    make(map[uuid.UUID]User),
		usersByEmail: make(map[string]User),
	}
}

func (f *fakeUserRepository) WithTx(tx pgx.Tx) UserRepository {
	return f
}

func (f *fakeUserRepository) Create(ctx context.Context, user *User) error {
	if f.createErr != nil {
		return f.createErr
	}

	if _, exists := f.usersByEmail[user.Email]; exists {
		return ErrUserEmailAlreadyExists
	}

	f.usersByID[user.ID] = *user
	f.usersByEmail[user.Email] = *user
	return nil
}

func (f *fakeUserRepository) GetByID(ctx context.Context, organisationID, userID uuid.UUID) (*User, error) {
	u, ok := f.usersByID[userID]
	if !ok || u.OrganisationID != organisationID {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

// GetByIDForAuthentication mirrors GetByID but with no organisation
// scoping, matching UserRepository's real contract for this method.
func (f *fakeUserRepository) GetByIDForAuthentication(ctx context.Context, userID uuid.UUID) (*User, error) {
	if f.getByIDForAuthenticationErr != nil {
		return nil, f.getByIDForAuthenticationErr
	}

	u, ok := f.usersByID[userID]
	if !ok {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

func (f *fakeUserRepository) GetByEmail(ctx context.Context, email string) (*User, error) {
	if f.getByEmailErr != nil {
		return nil, f.getByEmailErr
	}

	u, ok := f.usersByEmail[email]
	if !ok {
		return nil, ErrUserNotFound
	}
	return &u, nil
}

func (f *fakeUserRepository) UpdateLastLogin(ctx context.Context, userID uuid.UUID, at time.Time) error {
	if f.updateLastLoginErr != nil {
		return f.updateLastLoginErr
	}

	u, ok := f.usersByID[userID]
	if !ok {
		return ErrUserNotFound
	}

	atCopy := at
	u.LastLogin = &atCopy
	f.usersByID[userID] = u
	f.usersByEmail[u.Email] = u
	return nil
}

// List is a simple in-memory re-implementation of the same filter/sort/
// paginate contract PostgresUserRepository.List implements in SQL — good
// enough for handler-level tests; the real SQL predicates are proven
// separately by user_repository_postgres_test.go against real Postgres.
func (f *fakeUserRepository) List(ctx context.Context, organisationID uuid.UUID, filter UserListFilter) ([]*User, int64, error) {
	var matched []*User

	for i := range f.usersByID {
		u := f.usersByID[i]
		if u.OrganisationID != organisationID {
			continue
		}
		if filter.Role != "" && u.Role != filter.Role {
			continue
		}
		if filter.Active != nil && u.IsActive != *filter.Active {
			continue
		}
		uCopy := u
		matched = append(matched, &uCopy)
	}

	desc := strings.EqualFold(filter.Order, "desc")

	sort.Slice(matched, func(i, j int) bool {
		a, b := matched[i], matched[j]

		switch filter.Sort {
		case "email":
			if a.Email != b.Email {
				if desc {
					return a.Email > b.Email
				}
				return a.Email < b.Email
			}
		case "role":
			if a.Role != b.Role {
				if desc {
					return a.Role > b.Role
				}
				return a.Role < b.Role
			}
		default:
			if !a.CreatedAt.Equal(b.CreatedAt) {
				if desc {
					return a.CreatedAt.After(b.CreatedAt)
				}
				return a.CreatedAt.Before(b.CreatedAt)
			}
		}

		return a.ID.String() < b.ID.String()
	})

	total := int64(len(matched))

	start := filter.Offset
	if start > len(matched) {
		start = len(matched)
	}
	end := start + filter.Limit
	if end > len(matched) {
		end = len(matched)
	}

	return matched[start:end], total, nil
}

// userTestFixture bundles a UserService with fakes, pre-seeded with a
// valid organisation and a ready-to-use admin actor — most tests aren't
// about the actor/privilege-escalation rules themselves and just need
// *some* permitted actor to create a user as.
type userTestFixture struct {
	service                *UserService
	userRepository         *fakeUserRepository
	organisationRepository *fakeOrganisationRepository
	organisationID         uuid.UUID
	adminActor             CreateUserActor
}

func newUserTestFixture() *userTestFixture {
	organisationID := uuid.New()

	organisations := newFakeOrganisationRepository()
	organisations.organisations[organisationID] = Organisation{ID: organisationID, Name: "Test Organisation"}

	users := newFakeUserRepository()

	service := NewUserService(users, organisations)

	return &userTestFixture{
		service:                service,
		userRepository:         users,
		organisationRepository: organisations,
		organisationID:         organisationID,
		adminActor:             CreateUserActor{UserID: uuid.New(), Role: UserRoleAdmin},
	}
}

func newCreateUserRequest(name, email, password, role string) CreateUserRequest {
	return CreateUserRequest{Name: name, Email: email, Password: password, Role: role}
}

const validPassword = "correct horse battery staple" // 28 chars, well over the 12-char minimum

func TestUserService_Create_Success(t *testing.T) {
	f := newUserTestFixture()

	user, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice Example", "Alice@Example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if user.ID == uuid.Nil {
		t.Error("expected a generated user ID")
	}

	if user.OrganisationID != f.organisationID {
		t.Errorf("expected organisation ID %v, got %v", f.organisationID, user.OrganisationID)
	}

	if user.Name != "Alice Example" {
		t.Errorf("expected name %q, got %q", "Alice Example", user.Name)
	}

	if user.Email != "alice@example.com" {
		t.Errorf("expected normalised email %q, got %q", "alice@example.com", user.Email)
	}

	if user.Role != UserRoleUser {
		t.Errorf("expected default role %q, got %q", UserRoleUser, user.Role)
	}

	if !user.IsActive {
		t.Error("expected new user to be active")
	}
}

func TestUserService_Create_PasswordHashIsNotPlaintext(t *testing.T) {
	f := newUserTestFixture()

	user, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if user.PasswordHash == validPassword {
		t.Fatal("expected PasswordHash to be a hash, not the plaintext password")
	}

	if strings.Contains(user.PasswordHash, validPassword) {
		t.Fatal("expected PasswordHash not to contain the plaintext password")
	}
}

func TestUserService_Create_PasswordHashHasArgon2idFormat(t *testing.T) {
	f := newUserTestFixture()

	user, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if !strings.HasPrefix(user.PasswordHash, "$argon2id$v=19$m=19456,t=2,p=1$") {
		t.Errorf("expected an argon2id-formatted hash, got %q", user.PasswordHash)
	}

	// The stored hash must also be independently verifiable.
	if err := verifyPassword(validPassword, user.PasswordHash); err != nil {
		t.Errorf("expected the stored hash to verify against the original password, got %v", err)
	}
}

func TestUserService_Create_MissingName(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("   ", "alice@example.com", validPassword, ""))
	if !errors.Is(err, ErrUserNameRequired) {
		t.Fatalf("expected ErrUserNameRequired, got %v", err)
	}
}

func TestUserService_Create_MissingEmail(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "   ", validPassword, ""))
	if !errors.Is(err, ErrUserEmailRequired) {
		t.Fatalf("expected ErrUserEmailRequired, got %v", err)
	}
}

func TestUserService_Create_MissingPassword(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", "", ""))
	if !errors.Is(err, ErrUserPasswordRequired) {
		t.Fatalf("expected ErrUserPasswordRequired, got %v", err)
	}
}

func TestUserService_Create_PasswordShorterThanMinimum(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", "eleven-char", "")) // 11 characters
	if !errors.Is(err, ErrUserPasswordTooShort) {
		t.Fatalf("expected ErrUserPasswordTooShort, got %v", err)
	}
}

func TestUserService_Create_PasswordExactlyMinimumLengthSucceeds(t *testing.T) {
	f := newUserTestFixture()

	const twelveChars = "123456789012" // exactly 12 characters
	if len(twelveChars) != 12 {
		t.Fatalf("test setup error: expected a 12-character password, got %d", len(twelveChars))
	}

	if _, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", twelveChars, "")); err != nil {
		t.Fatalf("expected a 12-character password to be accepted, got %v", err)
	}
}

func TestUserService_Create_RoleDefaultsToUser(t *testing.T) {
	f := newUserTestFixture()

	user, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, "   "))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	if user.Role != UserRoleUser {
		t.Errorf("expected role to default to %q, got %q", UserRoleUser, user.Role)
	}
}

// TestUserService_Create_AdminCanAssignAnyRole proves an admin actor may
// create a user with any of the three valid roles, including admin and
// manager.
func TestUserService_Create_AdminCanAssignAnyRole(t *testing.T) {
	for _, role := range []string{UserRoleAdmin, UserRoleManager, UserRoleUser} {
		t.Run(role, func(t *testing.T) {
			f := newUserTestFixture()

			user, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice+"+role+"@example.com", validPassword, role))
			if err != nil {
				t.Fatalf("admin create user with role %q: %v", role, err)
			}

			if user.Role != role {
				t.Errorf("expected role %q, got %q", role, user.Role)
			}
		})
	}
}

func TestUserService_Create_InvalidRole(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, "superuser"))
	if !errors.Is(err, ErrUserRoleInvalid) {
		t.Fatalf("expected ErrUserRoleInvalid, got %v", err)
	}
}

func TestUserService_Create_OrganisationNotFound(t *testing.T) {
	f := newUserTestFixture()

	_, err := f.service.Create(context.Background(), uuid.New(), f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if !errors.Is(err, ErrOrganisationNotFound) {
		t.Fatalf("expected ErrOrganisationNotFound, got %v", err)
	}
}

func TestUserService_Create_DuplicateEmailPropagates(t *testing.T) {
	f := newUserTestFixture()

	if _, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, "")); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	_, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice Again", "alice@example.com", validPassword, ""))
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists, got %v", err)
	}
}

// TestUserService_Create_DuplicateEmailAcrossOrganisationsPropagates
// proves the Milestone 4 Part 2 semantic change at the service layer:
// since email uniqueness is now global, a second organisation cannot
// register a user with an email already used by a different
// organisation's user either.
func TestUserService_Create_DuplicateEmailAcrossOrganisationsPropagates(t *testing.T) {
	f := newUserTestFixture()

	if _, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, "")); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	otherOrganisationID := uuid.New()
	f.organisationRepository.organisations[otherOrganisationID] = Organisation{ID: otherOrganisationID, Name: "Other Organisation"}

	_, err := f.service.Create(context.Background(), otherOrganisationID, f.adminActor, newCreateUserRequest("Alice Again", "alice@example.com", validPassword, ""))
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists across organisations, got %v", err)
	}
}

// --- Privilege escalation / actor-role rules (Milestone 4 Part 5) ---
//
// These prove the rule lives in the service itself, not only in HTTP
// route middleware: every case below calls UserService.Create directly,
// the same way a background job, another transport, or a test would,
// with no HTTP layer involved at all.

func TestUserService_Create_ManagerCreatesUser_Succeeds(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	user, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleUser))
	if err != nil {
		t.Fatalf("manager create user: %v", err)
	}

	if user.Role != UserRoleUser {
		t.Errorf("expected role %q, got %q", UserRoleUser, user.Role)
	}
}

// TestUserService_Create_ManagerCreatesUser_RoleDefaultsToUser proves a
// manager can also rely on the same blank-role-defaults-to-user
// behaviour every other actor gets — a blank role is not itself an
// escalation attempt.
func TestUserService_Create_ManagerCreatesUser_RoleDefaultsToUser(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	user, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("manager create user: %v", err)
	}

	if user.Role != UserRoleUser {
		t.Errorf("expected role %q, got %q", UserRoleUser, user.Role)
	}
}

func TestUserService_Create_ManagerCannotCreateManager(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	_, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleManager))
	if !errors.Is(err, ErrUserRoleAssignmentNotPermitted) {
		t.Fatalf("expected ErrUserRoleAssignmentNotPermitted, got %v", err)
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

func TestUserService_Create_ManagerCannotCreateAdmin(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	_, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleAdmin))
	if !errors.Is(err, ErrUserRoleAssignmentNotPermitted) {
		t.Fatalf("expected ErrUserRoleAssignmentNotPermitted, got %v", err)
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// TestUserService_Create_RoleCasingIsNotNormalized_RejectedAsInvalid
// proves a differently-cased target role isn't silently treated as its
// lowercase equivalent: role matching is deliberately exact-case (see
// validateTargetRole), so "Admin"/"ADMIN"/"Manager"/"MANAGER" fail
// target-role validation entirely — the wire value isn't a recognised
// role at all — rather than ever reaching, let alone bypassing, the
// manager-escalation check below. This is the Milestone 4 Part 6 audit's
// casing finding, made explicit as a regression test.
func TestUserService_Create_RoleCasingIsNotNormalized_RejectedAsInvalid(t *testing.T) {
	for _, role := range []string{"Admin", "ADMIN", "Manager", "MANAGER", "User", "USER"} {
		t.Run(role, func(t *testing.T) {
			f := newUserTestFixture()
			manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

			_, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice+"+role+"@example.com", validPassword, role))
			if !errors.Is(err, ErrUserRoleInvalid) {
				t.Fatalf("expected ErrUserRoleInvalid for unrecognised-cased role %q (roles are not case-normalized), got %v", role, err)
			}

			if len(f.userRepository.usersByID) != 0 {
				t.Error("expected no user to have been created")
			}
		})
	}
}

// TestUserService_Create_ManagerCannotCreateManager_WithWhitespacePadding
// proves whitespace trimming (an intentional normalization — see
// validateTargetRole) doesn't accidentally let a manager create another
// manager: " manager " still resolves to exactly "manager", which is
// still not UserRoleUser, so the escalation rule still applies.
func TestUserService_Create_ManagerCannotCreateManager_WithWhitespacePadding(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	_, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, "  manager  "))
	if !errors.Is(err, ErrUserRoleAssignmentNotPermitted) {
		t.Fatalf("expected ErrUserRoleAssignmentNotPermitted for a whitespace-padded \"manager\" role, got %v", err)
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// TestUserService_Create_ManagerBlankRole_DefaultsToUserBeforeAuthorization
// makes explicit what TestUserService_Create_ManagerCreatesUser_RoleDefaultsToUser
// already proves: validateTargetRole's blank-defaults-to-user step runs
// strictly before the actor-permission switch, so a manager submitting a
// blank role is evaluated as "manager creating user" (permitted), never
// as "manager creating an unvalidated blank role" (which could otherwise
// be ambiguous). Kept as its own test, separate from the success-path
// one, specifically to document the ordering as a security property.
func TestUserService_Create_ManagerBlankRole_DefaultsToUserBeforeAuthorization(t *testing.T) {
	f := newUserTestFixture()
	manager := CreateUserActor{UserID: uuid.New(), Role: UserRoleManager}

	user, err := f.service.Create(context.Background(), f.organisationID, manager, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("expected a blank role from a manager to default to user and succeed, got %v", err)
	}

	if user.Role != UserRoleUser {
		t.Errorf("expected role %q, got %q", UserRoleUser, user.Role)
	}
}

func TestUserService_Create_UserActorCannotCreateAnyone(t *testing.T) {
	f := newUserTestFixture()
	actor := CreateUserActor{UserID: uuid.New(), Role: UserRoleUser}

	_, err := f.service.Create(context.Background(), f.organisationID, actor, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleUser))
	if !errors.Is(err, ErrUserActorNotPermittedToCreateUsers) {
		t.Fatalf("expected ErrUserActorNotPermittedToCreateUsers, got %v", err)
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// TestUserService_Create_UnknownActorRoleCannotCreateAnyone proves an
// unrecognised actor role fails closed — it must not be silently treated
// as equivalent to UserRoleUser (which would happen to be rejected too,
// but for the wrong reason) or, worse, as an implicitly trusted role.
func TestUserService_Create_UnknownActorRoleCannotCreateAnyone(t *testing.T) {
	f := newUserTestFixture()
	actor := CreateUserActor{UserID: uuid.New(), Role: "superadmin"}

	_, err := f.service.Create(context.Background(), f.organisationID, actor, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleUser))
	if !errors.Is(err, ErrUserActorNotPermittedToCreateUsers) {
		t.Fatalf("expected ErrUserActorNotPermittedToCreateUsers, got %v", err)
	}

	if len(f.userRepository.usersByID) != 0 {
		t.Error("expected no user to have been created")
	}
}

// TestUserService_Create_EmptyActorRoleCannotCreateAnyone covers the
// zero-value CreateUserActor case specifically (as opposed to an
// unrecognised non-empty string).
func TestUserService_Create_EmptyActorRoleCannotCreateAnyone(t *testing.T) {
	f := newUserTestFixture()
	actor := CreateUserActor{UserID: uuid.New()} // Role left as its zero value ""

	_, err := f.service.Create(context.Background(), f.organisationID, actor, newCreateUserRequest("Alice", "alice@example.com", validPassword, UserRoleUser))
	if !errors.Is(err, ErrUserActorNotPermittedToCreateUsers) {
		t.Fatalf("expected ErrUserActorNotPermittedToCreateUsers, got %v", err)
	}
}

func TestUserService_GetByID(t *testing.T) {
	f := newUserTestFixture()

	created, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	got, err := f.service.GetByID(context.Background(), f.organisationID, created.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if got.ID != created.ID {
		t.Errorf("expected ID %v, got %v", created.ID, got.ID)
	}
}

func TestUserService_GetByID_WrongOrganisation(t *testing.T) {
	f := newUserTestFixture()

	created, err := f.service.Create(context.Background(), f.organisationID, f.adminActor, newCreateUserRequest("Alice", "alice@example.com", validPassword, ""))
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	_, err = f.service.GetByID(context.Background(), uuid.New(), created.ID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound for a cross-organisation lookup, got %v", err)
	}
}
