package admin

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

func newTestUser(organisationID uuid.UUID, email string) *User {
	return &User{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Test User",
		Email:          email,
		PasswordHash:   "$argon2id$v=19$m=19456,t=2,p=1$c29tZXNhbHQ$c29tZWtleQ",
		Role:           UserRoleUser,
		IsActive:       true,
	}
}

func TestPostgresUserRepository_CreateAndGetByID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	user := newTestUser(organisationID, "alice@example.com")

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	got, err := repository.GetByID(ctx, organisationID, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if got.ID != user.ID {
		t.Errorf("expected ID %v, got %v", user.ID, got.ID)
	}

	if got.Email != "alice@example.com" {
		t.Errorf("expected email %q, got %q", "alice@example.com", got.Email)
	}

	if got.PasswordHash != user.PasswordHash {
		t.Errorf("expected password hash to round-trip unchanged")
	}

	if got.Role != UserRoleUser {
		t.Errorf("expected role %q, got %q", UserRoleUser, got.Role)
	}

	if !got.IsActive {
		t.Error("expected user to be active")
	}

	if got.LastLogin != nil {
		t.Errorf("expected LastLogin to be nil for a new user, got %v", *got.LastLogin)
	}
}

func TestPostgresUserRepository_GetByID_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	got, err := repository.GetByID(ctx, organisationID, uuid.New())
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if got != nil {
		t.Errorf("expected nil user, got %+v", got)
	}
}

// TestPostgresUserRepository_GetByID_OrganisationScoping proves that a
// user belonging to one organisation cannot be retrieved through a
// different organisation's ID.
func TestPostgresUserRepository_GetByID_OrganisationScoping(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	user := newTestUser(organisationA, "bob@example.com")

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	if _, err := repository.GetByID(ctx, organisationA, user.ID); err != nil {
		t.Fatalf("get user via owning organisation: %v", err)
	}

	result, err := repository.GetByID(ctx, organisationB, user.ID)
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound when scoped to the wrong organisation, got %v", err)
	}

	if result != nil {
		t.Errorf("expected nil user, got %+v", result)
	}
}

func TestPostgresUserRepository_GetByEmail_Found(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	user := newTestUser(organisationID, "carol@example.com")

	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	got, err := repository.GetByEmail(ctx, "carol@example.com")
	if err != nil {
		t.Fatalf("get user by email: %v", err)
	}

	if got.ID != user.ID {
		t.Errorf("expected ID %v, got %v", user.ID, got.ID)
	}
}

func TestPostgresUserRepository_GetByEmail_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresUserRepository(db)

	got, err := repository.GetByEmail(ctx, "nobody@example.com")
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}

	if got != nil {
		t.Errorf("expected nil user, got %+v", got)
	}
}

func TestPostgresUserRepository_Create_DuplicateEmail(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	first := newTestUser(organisationID, "dupe@example.com")
	if err := repository.Create(ctx, first); err != nil {
		t.Fatalf("create first user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", first.ID)
	})

	second := newTestUser(organisationID, "dupe@example.com")
	err := repository.Create(ctx, second)
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists, got %v", err)
	}
}

// TestPostgresUserRepository_Create_SameEmailAcrossOrganisationsIsRejected
// proves the Milestone 4 Part 2 schema change: users_email_unique makes
// email uniqueness global, so a second organisation can no longer reuse
// an email already used by a different organisation's user — the
// opposite of this repository's pre-Part-2 behaviour.
func TestPostgresUserRepository_Create_SameEmailAcrossOrganisationsIsRejected(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationA := createTestOrganisation(t, db)
	organisationB := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	userA := newTestUser(organisationA, "shared@example.com")
	if err := repository.Create(ctx, userA); err != nil {
		t.Fatalf("create user in organisation A: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", userA.ID)
	})

	userB := newTestUser(organisationB, "shared@example.com")
	err := repository.Create(ctx, userB)
	if !errors.Is(err, ErrUserEmailAlreadyExists) {
		t.Fatalf("expected ErrUserEmailAlreadyExists across organisations, got %v", err)
	}
}

func TestPostgresUserRepository_UpdateLastLogin(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	repository := NewPostgresUserRepository(db)

	user := newTestUser(organisationID, "dana@example.com")
	if err := repository.Create(ctx, user); err != nil {
		t.Fatalf("create user: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM users WHERE id = $1", user.ID)
	})

	loggedInAt := time.Now().UTC().Truncate(time.Microsecond)
	if err := repository.UpdateLastLogin(ctx, user.ID, loggedInAt); err != nil {
		t.Fatalf("update last login: %v", err)
	}

	got, err := repository.GetByID(ctx, organisationID, user.ID)
	if err != nil {
		t.Fatalf("get user: %v", err)
	}

	if got.LastLogin == nil {
		t.Fatal("expected LastLogin to be set")
	}

	if !got.LastLogin.Equal(loggedInAt) {
		t.Errorf("expected LastLogin %v, got %v", loggedInAt, *got.LastLogin)
	}
}

func TestPostgresUserRepository_UpdateLastLogin_NotFound(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	repository := NewPostgresUserRepository(db)

	err := repository.UpdateLastLogin(ctx, uuid.New(), time.Now().UTC())
	if !errors.Is(err, ErrUserNotFound) {
		t.Fatalf("expected ErrUserNotFound, got %v", err)
	}
}
