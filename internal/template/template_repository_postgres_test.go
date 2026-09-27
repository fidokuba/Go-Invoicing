package template

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is not set")
	}

	db, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("create database pool: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	return db
}

// createTestOrganisation inserts a minimal organisation row directly (not
// via the administration package, to avoid a cross-package test
// dependency — the same reasoning internal/invoice's own copy of this
// helper already documents) and registers its cleanup. The cleanup for
// any invoice_templates row a test creates against this organisation
// must be registered separately, and before this call returns to the
// caller's own further t.Cleanup registrations — see each test below,
// which does so immediately after calling this — since there is no
// ON DELETE CASCADE from invoice_templates to organisations and
// t.Cleanup runs last-registered-first.
func createTestOrganisation(t *testing.T, db *pgxpool.Pool) uuid.UUID {
	t.Helper()

	organisationID := uuid.New()

	_, err := db.Exec(
		context.Background(),
		"INSERT INTO organisations (id, name) VALUES ($1, $2)",
		organisationID, "Test Organisation",
	)
	if err != nil {
		t.Fatalf("create test organisation: %v", err)
	}

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM organisations WHERE id = $1", organisationID)
	})

	return organisationID
}

// cleanupTemplates registers deletion of every invoice_templates row for
// organisationID. Must be registered (via this call) after
// createTestOrganisation's own cleanup so it runs first (t.Cleanup is
// LIFO), or the organisation delete would fail against the still-present
// foreign key.
func cleanupTemplates(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoice_templates WHERE organisation_id = $1", organisationID)
	})
}

func TestPostgresTemplateRepository_CreateAndGetByID(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	repository := NewPostgresTemplateRepository(db)
	tmpl := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{"content":[]}`)}

	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}
	if tmpl.Version != 1 {
		t.Errorf("expected a new template to start at version 1, got %d", tmpl.Version)
	}

	got, err := repository.GetByID(ctx, orgID, tmpl.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Name != "My Layout" {
		t.Errorf("expected name %q, got %q", "My Layout", got.Name)
	}
	// jsonb reformats on round-trip (whitespace, key order) — this
	// project's first JSON column, so worth spelling out: compare
	// parsed structure, never the raw bytes, the same as any other
	// jsonb-backed field would have to.
	var gotDefinition, wantDefinition map[string]any
	if err := json.Unmarshal(got.Definition, &gotDefinition); err != nil {
		t.Fatalf("unmarshal got definition: %v", err)
	}
	if err := json.Unmarshal(tmpl.Definition, &wantDefinition); err != nil {
		t.Fatalf("unmarshal want definition: %v", err)
	}
	if !reflect.DeepEqual(gotDefinition, wantDefinition) {
		t.Errorf("expected definition to round-trip with the same structure, got %s, want %s", got.Definition, tmpl.Definition)
	}
	if got.IsDefault || got.IsSystem {
		t.Errorf("expected a plain created template to be neither default nor system, got %+v", got)
	}
}

func TestPostgresTemplateRepository_GetByID_NotFound(t *testing.T) {
	db := newTestPool(t)
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	repository := NewPostgresTemplateRepository(db)

	_, err := repository.GetByID(context.Background(), orgID, uuid.New())
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

// TestPostgresTemplateRepository_GetByID_TenantIsolation proves a
// template belonging to a different organisation is indistinguishable
// from one that doesn't exist at all — the same tenant-isolation
// discipline every other repository's GetByID already guarantees.
func TestPostgresTemplateRepository_GetByID_TenantIsolation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgA := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgA)
	orgB := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgB)

	repository := NewPostgresTemplateRepository(db)
	tmpl := &Template{ID: uuid.New(), OrganisationID: orgA, Name: "Org A's Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}

	if _, err := repository.GetByID(ctx, orgB, tmpl.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound reading another organisation's template, got %v", err)
	}
}

func TestPostgresTemplateRepository_GetSystemTemplate(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	repository := NewPostgresTemplateRepository(db)
	system, err := repository.GetSystemTemplate(ctx, orgID)
	if err != nil {
		t.Fatalf("get system template: %v", err)
	}
	if system.Name != ClassicTemplateName {
		t.Errorf("expected name %q, got %q", ClassicTemplateName, system.Name)
	}
	if !system.IsDefault || !system.IsSystem {
		t.Errorf("expected the seeded classic template to be both default and system, got %+v", system)
	}
}

// TestPostgresTemplateRepository_GetDefault_FollowsSetDefault proves
// GetDefault always reflects whichever template is currently marked
// default — Classic to start with (the only row that exists), then a
// user-created template once SetDefault has switched to it. This is the
// exact lookup InvoicePDFService (Phase 4) and InvoiceService.Send rely
// on: "what does this organisation's PDF look like right now."
func TestPostgresTemplateRepository_GetDefault_FollowsSetDefault(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	repository := NewPostgresTemplateRepository(db)

	byDefault, err := repository.GetDefault(ctx, orgID)
	if err != nil {
		t.Fatalf("get default (classic): %v", err)
	}
	if !byDefault.IsSystem {
		t.Errorf("expected the default to initially be the system template, got %+v", byDefault)
	}

	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repository.SetDefault(ctx, orgID, created.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}

	byDefault, err = repository.GetDefault(ctx, orgID)
	if err != nil {
		t.Fatalf("get default (after switch): %v", err)
	}
	if byDefault.ID != created.ID {
		t.Errorf("expected the default to now be %v, got %+v", created.ID, byDefault)
	}
}

func TestPostgresTemplateRepository_List_SystemTemplateFirst(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	repository := NewPostgresTemplateRepository(db)
	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}

	templates, err := repository.List(ctx, orgID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}
	if !templates[0].IsSystem {
		t.Errorf("expected the system template first, got %+v", templates[0])
	}
	if templates[1].ID != created.ID {
		t.Errorf("expected the user-created template second, got %+v", templates[1])
	}
}

func TestPostgresTemplateRepository_Update_Success(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	repository := NewPostgresTemplateRepository(db)
	tmpl := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}

	update := &Template{ID: tmpl.ID, Name: "Renamed", Definition: json.RawMessage(`{"changed":true}`)}
	if err := repository.Update(ctx, orgID, update, tmpl.Version); err != nil {
		t.Fatalf("update: %v", err)
	}
	if update.Version != tmpl.Version+1 {
		t.Errorf("expected version %d, got %d", tmpl.Version+1, update.Version)
	}

	got, err := repository.GetByID(ctx, orgID, tmpl.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.Name != "Renamed" {
		t.Errorf("expected name %q, got %q", "Renamed", got.Name)
	}
}

func TestPostgresTemplateRepository_Update_VersionConflict(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	repository := NewPostgresTemplateRepository(db)
	tmpl := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}

	update := &Template{ID: tmpl.ID, Name: "Renamed", Definition: json.RawMessage(`{}`)}
	err := repository.Update(ctx, orgID, update, tmpl.Version+1)
	if !errors.Is(err, ErrTemplateVersionConflict) {
		t.Fatalf("expected ErrTemplateVersionConflict, got %v", err)
	}
}

func TestPostgresTemplateRepository_SoftDelete(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	repository := NewPostgresTemplateRepository(db)
	tmpl := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repository.SoftDelete(ctx, orgID, tmpl.ID); err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	if _, err := repository.GetByID(ctx, orgID, tmpl.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected a soft-deleted template to read as ErrTemplateNotFound, got %v", err)
	}

	var deletedAtIsSet bool
	if err := db.QueryRow(ctx, "SELECT deleted_at IS NOT NULL FROM invoice_templates WHERE id = $1", tmpl.ID).Scan(&deletedAtIsSet); err != nil {
		t.Fatalf("check deleted_at: %v", err)
	}
	if !deletedAtIsSet {
		t.Error("expected deleted_at to be set — a real soft delete, not a hard delete")
	}
}

// TestPostgresTemplateRepository_SetDefault_AtomicSwitch proves the real
// invariant this whole feature depends on: SetDefault's two UPDATEs
// never produce a moment (or a final state) with more than one default
// template for the organisation, enforced by
// invoice_templates_one_default_per_org — a real PostgreSQL partial
// unique index, not application-level locking.
func TestPostgresTemplateRepository_SetDefault_AtomicSwitch(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	repository := NewPostgresTemplateRepository(db)
	created := &Template{ID: uuid.New(), OrganisationID: orgID, Name: "My Layout", Definition: json.RawMessage(`{}`)}
	if err := repository.Create(ctx, created); err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := repository.SetDefault(ctx, orgID, created.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}

	var defaultCount int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM invoice_templates WHERE organisation_id = $1 AND is_default = TRUE", orgID).Scan(&defaultCount); err != nil {
		t.Fatalf("count defaults: %v", err)
	}
	if defaultCount != 1 {
		t.Fatalf("expected exactly 1 default template, got %d", defaultCount)
	}

	got, err := repository.GetByID(ctx, orgID, created.ID)
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if !got.IsDefault {
		t.Error("expected the newly-set template to be the default")
	}
}

// TestPostgresTemplateRepository_OneDefaultPerOrganisation_EnforcedByDatabase
// proves invoice_templates_one_default_per_org is real — bypassing
// SetDefault entirely and attempting a raw second is_default=TRUE INSERT
// directly, which must be the database itself, not application code,
// that refuses it.
func TestPostgresTemplateRepository_OneDefaultPerOrganisation_EnforcedByDatabase(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("commit: %v", err)
	}

	_, err = db.Exec(ctx,
		`INSERT INTO invoice_templates (id, organisation_id, name, definition, is_default) VALUES ($1, $2, 'Second Default', '{}'::jsonb, TRUE)`,
		uuid.New(), orgID,
	)
	if err == nil {
		t.Fatal("expected the database to reject a second default template for the same organisation")
	}
}

func TestCreateSystemTemplate_RollsBackWithItsTransaction(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()
	orgID := createTestOrganisation(t, db)
	cleanupTemplates(t, db, orgID)

	tx, err := db.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	if err := CreateSystemTemplate(ctx, tx, orgID); err != nil {
		t.Fatalf("create system template: %v", err)
	}
	if err := tx.Rollback(ctx); err != nil {
		t.Fatalf("rollback: %v", err)
	}

	var count int
	if err := db.QueryRow(ctx, "SELECT count(*) FROM invoice_templates WHERE organisation_id = $1", orgID).Scan(&count); err != nil {
		t.Fatalf("count templates: %v", err)
	}
	if count != 0 {
		t.Errorf("expected the rolled-back transaction to leave no template row, got %d", count)
	}
}
