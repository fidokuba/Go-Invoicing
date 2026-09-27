package template

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// fakeTemplateRepository is an in-memory TemplateRepository, the same
// role fakeSettingsRepository/fakeOrganisationRepository play in
// internal/administration's own tests: fast, no PostgreSQL, but real
// enough business behaviour (tenant scoping, version conflicts,
// "exactly one default") to exercise TemplateService's actual logic.
// WithTx ignores its tx argument and returns the same fake — it has no
// real transactional semantics, so a test proving TemplateService.Delete
// rolled back on a mid-transaction failure isn't provable against this
// fake at all — see template_repository_postgres_test.go for that.
type fakeTemplateRepository struct {
	mu        sync.Mutex
	templates []*Template

	createErr     error
	listErr       error
	updateErr     error
	softDeleteErr error
	setDefaultErr error
}

func newFakeTemplateRepository() *fakeTemplateRepository {
	return &fakeTemplateRepository{}
}

func (f *fakeTemplateRepository) WithTx(tx pgx.Tx) TemplateRepository {
	return f
}

func (f *fakeTemplateRepository) Create(ctx context.Context, t *Template) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.createErr != nil {
		return f.createErr
	}

	now := time.Now()
	t.CreatedAt = now
	t.UpdatedAt = now
	t.Version = 1

	copy := *t
	f.templates = append(f.templates, &copy)
	return nil
}

func (f *fakeTemplateRepository) find(organisationID, id uuid.UUID) *Template {
	for _, t := range f.templates {
		if t.ID == id && t.OrganisationID == organisationID && t.DeletedAt == nil {
			return t
		}
	}
	return nil
}

func (f *fakeTemplateRepository) GetByID(ctx context.Context, organisationID, id uuid.UUID) (*Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	t := f.find(organisationID, id)
	if t == nil {
		return nil, ErrTemplateNotFound
	}
	copy := *t
	return &copy, nil
}

func (f *fakeTemplateRepository) GetSystemTemplate(ctx context.Context, organisationID uuid.UUID) (*Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, t := range f.templates {
		if t.OrganisationID == organisationID && t.IsSystem && t.DeletedAt == nil {
			copy := *t
			return &copy, nil
		}
	}
	return nil, ErrTemplateNotFound
}

func (f *fakeTemplateRepository) GetDefault(ctx context.Context, organisationID uuid.UUID) (*Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, t := range f.templates {
		if t.OrganisationID == organisationID && t.IsDefault && t.DeletedAt == nil {
			copy := *t
			return &copy, nil
		}
	}
	return nil, ErrTemplateNotFound
}

func (f *fakeTemplateRepository) List(ctx context.Context, organisationID uuid.UUID) ([]*Template, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.listErr != nil {
		return nil, f.listErr
	}

	// Matches the real repository's ORDER BY is_system DESC, created_at
	// ASC: templates are appended in creation order, so a single pass
	// putting the system template first (there is only ever one) and
	// everything else in append order already satisfies that ordering
	// without needing an actual sort.
	var system *Template
	var rest []*Template
	for _, t := range f.templates {
		if t.OrganisationID != organisationID || t.DeletedAt != nil {
			continue
		}
		copy := *t
		if t.IsSystem {
			system = &copy
		} else {
			rest = append(rest, &copy)
		}
	}

	var result []*Template
	if system != nil {
		result = append(result, system)
	}
	return append(result, rest...), nil
}

func (f *fakeTemplateRepository) Update(ctx context.Context, organisationID uuid.UUID, t *Template, expectedVersion int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.updateErr != nil {
		return f.updateErr
	}

	existing := f.find(organisationID, t.ID)
	if existing == nil {
		return ErrTemplateNotFound
	}
	if existing.Version != expectedVersion {
		return ErrTemplateVersionConflict
	}

	existing.Name = t.Name
	existing.Definition = t.Definition
	existing.Version++
	existing.UpdatedAt = time.Now()

	t.Version = existing.Version
	t.UpdatedAt = existing.UpdatedAt
	return nil
}

func (f *fakeTemplateRepository) SoftDelete(ctx context.Context, organisationID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.softDeleteErr != nil {
		return f.softDeleteErr
	}

	existing := f.find(organisationID, id)
	if existing == nil {
		return ErrTemplateNotFound
	}

	now := time.Now()
	existing.DeletedAt = &now
	return nil
}

func (f *fakeTemplateRepository) SetDefault(ctx context.Context, organisationID, id uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.setDefaultErr != nil {
		return f.setDefaultErr
	}

	target := f.find(organisationID, id)
	if target == nil {
		return ErrTemplateNotFound
	}

	for _, t := range f.templates {
		if t.OrganisationID == organisationID && t.DeletedAt == nil {
			t.IsDefault = false
		}
	}
	target.IsDefault = true
	return nil
}

// fakeTx/fakeTxBeginner mirror internal/administration's own fakes
// exactly (same field names, same behaviour, right down to Rollback
// being a no-op once Commit has already closed the transaction) —
// TemplateService.Delete needs a real TxBeginner the same way
// RegistrationService.Register does, to prove its multi-step
// orchestration commits or rolls back as one unit. Every pgx.Tx method
// is implemented explicitly (not embedded) so a call this package's
// code doesn't expect to make would fail loudly rather than nil-panic.
type fakeTx struct {
	committed  bool
	rolledBack bool
	commitErr  error
}

func (t *fakeTx) Commit(ctx context.Context) error {
	t.committed = true
	return t.commitErr
}

func (t *fakeTx) Rollback(ctx context.Context) error {
	if t.committed {
		return pgx.ErrTxClosed
	}
	t.rolledBack = true
	return nil
}

func (t *fakeTx) Begin(ctx context.Context) (pgx.Tx, error) { return nil, nil }
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

type fakeTxBeginner struct {
	tx             *fakeTx
	beginErr       error
	beginCallCount int
}

func (b *fakeTxBeginner) Begin(ctx context.Context) (pgx.Tx, error) {
	b.beginCallCount++
	if b.beginErr != nil {
		return nil, b.beginErr
	}
	return b.tx, nil
}
