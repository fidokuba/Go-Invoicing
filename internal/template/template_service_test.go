package template

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/uuid"
)

type templateTestFixture struct {
	service    *TemplateService
	repository *fakeTemplateRepository
	tx         *fakeTx
	txBeginner *fakeTxBeginner
}

func newTemplateTestFixture() *templateTestFixture {
	repository := newFakeTemplateRepository()
	tx := &fakeTx{}
	txBeginner := &fakeTxBeginner{tx: tx}

	return &templateTestFixture{
		service:    NewTemplateService(repository, txBeginner),
		repository: repository,
		tx:         tx,
		txBeginner: txBeginner,
	}
}

// seedSystemTemplate mirrors what migration 000020's backfill (or
// RegistrationService.Register, for a newer organisation) already
// guarantees exists before any of this package's own code ever runs:
// every organisation has exactly one Classic template, default from
// the start.
func (f *templateTestFixture) seedSystemTemplate(organisationID uuid.UUID) *Template {
	t := &Template{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           ClassicTemplateName,
		Definition:     json.RawMessage(`{"system":"classic"}`),
		IsDefault:      true,
		IsSystem:       true,
	}
	if err := f.repository.Create(context.Background(), t); err != nil {
		panic(err)
	}
	return t
}

var sampleDefinition = json.RawMessage(`{"content":[],"root":{}}`)

func TestTemplateService_Create_Success(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if created.IsDefault {
		t.Error("expected a newly created template not to be the default")
	}
	if created.IsSystem {
		t.Error("expected a newly created template not to be a system template")
	}
	if created.OrganisationID != orgID {
		t.Errorf("expected organisation ID %v, got %v", orgID, created.OrganisationID)
	}
}

func TestTemplateService_Create_NameRequired(t *testing.T) {
	f := newTemplateTestFixture()

	_, err := f.service.Create(context.Background(), uuid.New(), "   ", sampleDefinition)
	if !errors.Is(err, ErrTemplateNameRequired) {
		t.Fatalf("expected ErrTemplateNameRequired, got %v", err)
	}
}

func TestTemplateService_Create_DefinitionTooLarge(t *testing.T) {
	f := newTemplateTestFixture()

	oversized := make([]byte, maxDefinitionBytes+1)
	for i := range oversized {
		oversized[i] = 'x'
	}

	_, err := f.service.Create(context.Background(), uuid.New(), "My Layout", json.RawMessage(oversized))
	if !errors.Is(err, ErrTemplateDefinitionTooLarge) {
		t.Fatalf("expected ErrTemplateDefinitionTooLarge, got %v", err)
	}
}

func TestTemplateService_List_SystemTemplateFirst(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	system := f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	templates, err := f.service.List(context.Background(), orgID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	if len(templates) != 2 {
		t.Fatalf("expected 2 templates, got %d", len(templates))
	}
	if templates[0].ID != system.ID {
		t.Errorf("expected the system template first, got %+v", templates[0])
	}
	if templates[1].ID != created.ID {
		t.Errorf("expected the user template second, got %+v", templates[1])
	}
}

func TestTemplateService_Update_Success(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	updated, err := f.service.Update(context.Background(), orgID, created.ID, "Renamed", sampleDefinition, created.Version)
	if err != nil {
		t.Fatalf("update: %v", err)
	}

	if updated.Name != "Renamed" {
		t.Errorf("expected name %q, got %q", "Renamed", updated.Name)
	}
	if updated.Version != created.Version+1 {
		t.Errorf("expected version %d, got %d", created.Version+1, updated.Version)
	}
}

func TestTemplateService_Update_VersionConflict(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	_, err = f.service.Update(context.Background(), orgID, created.ID, "Renamed", sampleDefinition, created.Version+1)
	if !errors.Is(err, ErrTemplateVersionConflict) {
		t.Fatalf("expected ErrTemplateVersionConflict, got %v", err)
	}
}

// TestTemplateService_Update_SystemTemplateRejected proves the system
// (Classic) template is read-only, not just non-deletable — see
// TemplateService.Update's own doc comment for why.
func TestTemplateService_Update_SystemTemplateRejected(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	system := f.seedSystemTemplate(orgID)

	_, err := f.service.Update(context.Background(), orgID, system.ID, "Renamed Classic", sampleDefinition, system.Version)
	if !errors.Is(err, ErrTemplateIsSystemCannotBeDeleted) {
		t.Fatalf("expected ErrTemplateIsSystemCannotBeDeleted, got %v", err)
	}
}

func TestTemplateService_Update_NotFound(t *testing.T) {
	f := newTemplateTestFixture()

	_, err := f.service.Update(context.Background(), uuid.New(), uuid.New(), "Renamed", sampleDefinition, 1)
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

// TestTemplateService_Delete_SystemTemplateRejected proves the one rule
// that actually matters here: no code path can delete the permanent
// Classic template, regardless of its current IsDefault status.
func TestTemplateService_Delete_SystemTemplateRejected(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	system := f.seedSystemTemplate(orgID)

	err := f.service.Delete(context.Background(), orgID, system.ID)
	if !errors.Is(err, ErrTemplateIsSystemCannotBeDeleted) {
		t.Fatalf("expected ErrTemplateIsSystemCannotBeDeleted, got %v", err)
	}

	if _, err := f.repository.GetByID(context.Background(), orgID, system.ID); err != nil {
		t.Errorf("expected the system template to still exist, got %v", err)
	}
}

func TestTemplateService_Delete_NonDefaultTemplate(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := f.service.Delete(context.Background(), orgID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	if _, err := f.repository.GetByID(context.Background(), orgID, created.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("expected the deleted template to be gone, got %v", err)
	}
	if !f.tx.committed {
		t.Error("expected the transaction to be committed")
	}
}

// TestTemplateService_Delete_CurrentDefaultRevertsToClassic is the
// central business rule this whole package exists to enforce: deleting
// the organisation's current default template must never leave it with
// zero defaults — it reverts to Classic, atomically with the delete.
func TestTemplateService_Delete_CurrentDefaultRevertsToClassic(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	system := f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.service.SetDefault(context.Background(), orgID, created.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}

	if err := f.service.Delete(context.Background(), orgID, created.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	reverted, err := f.repository.GetByID(context.Background(), orgID, system.ID)
	if err != nil {
		t.Fatalf("get classic template: %v", err)
	}
	if !reverted.IsDefault {
		t.Error("expected the organisation's default to have reverted to the Classic template")
	}
}

func TestTemplateService_Delete_NotFound(t *testing.T) {
	f := newTemplateTestFixture()

	err := f.service.Delete(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

// TestTemplateService_Delete_RollsBackOnSoftDeleteFailure proves Delete
// is a real transaction, not two independent writes: if the delete
// itself fails, nothing (including a since-abandoned default-reversion)
// is left half-applied.
func TestTemplateService_Delete_RollsBackOnSoftDeleteFailure(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	f.repository.softDeleteErr = errors.New("connection reset by peer")

	if err := f.service.Delete(context.Background(), orgID, created.ID); err == nil {
		t.Fatal("expected an error")
	}

	if !f.tx.rolledBack {
		t.Error("expected the transaction to be rolled back")
	}
	if f.tx.committed {
		t.Error("expected the transaction not to be committed")
	}
}

func TestTemplateService_SetDefault_Success(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	if err := f.service.SetDefault(context.Background(), orgID, created.ID); err != nil {
		t.Fatalf("set default: %v", err)
	}

	templates, err := f.repository.List(context.Background(), orgID)
	if err != nil {
		t.Fatalf("list: %v", err)
	}

	var defaultCount int
	for _, tmpl := range templates {
		if tmpl.IsDefault {
			defaultCount++
			if tmpl.ID != created.ID {
				t.Errorf("expected %v to be the only default, but %v is also default", created.ID, tmpl.ID)
			}
		}
	}
	if defaultCount != 1 {
		t.Errorf("expected exactly 1 default template, got %d", defaultCount)
	}
}

func TestTemplateService_SetDefault_NotFound(t *testing.T) {
	f := newTemplateTestFixture()

	err := f.service.SetDefault(context.Background(), uuid.New(), uuid.New())
	if !errors.Is(err, ErrTemplateNotFound) {
		t.Fatalf("expected ErrTemplateNotFound, got %v", err)
	}
}

// TestTemplateService_SetDefault_CanReturnToSystemTemplate proves an
// organisation can deliberately switch back to Classic (not only via
// Delete's automatic fallback) — SetDefault has no "never the system
// template" restriction the way Update/Delete do.
func TestTemplateService_SetDefault_CanReturnToSystemTemplate(t *testing.T) {
	f := newTemplateTestFixture()
	orgID := uuid.New()
	system := f.seedSystemTemplate(orgID)

	created, err := f.service.Create(context.Background(), orgID, "My Layout", sampleDefinition)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := f.service.SetDefault(context.Background(), orgID, created.ID); err != nil {
		t.Fatalf("set default to created: %v", err)
	}

	if err := f.service.SetDefault(context.Background(), orgID, system.ID); err != nil {
		t.Fatalf("set default back to classic: %v", err)
	}

	reverted, err := f.repository.GetByID(context.Background(), orgID, system.ID)
	if err != nil {
		t.Fatalf("get classic template: %v", err)
	}
	if !reverted.IsDefault {
		t.Error("expected the classic template to be the default again")
	}
}
