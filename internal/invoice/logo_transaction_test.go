package invoice

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/template"
)

// testLogoPNG is a valid 1x1 PNG.
var testLogoPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

func uploadTestLogo(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) *admin.OrganisationLogo {
	t.Helper()
	service := admin.NewOrganisationService(admin.NewPostgresOrganisationRepository(db), admin.NewPostgresSettingsRepository(db))
	logo, err := service.SetLogo(context.Background(), organisationID, testLogoPNG)
	if err != nil {
		t.Fatalf("upload logo: %v", err)
	}
	return logo
}

// useCustomTestLayout makes a minimal custom layout the organisation's
// default, so Generate takes the renderer path that embeds the logo.
func useCustomTestLayout(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	repository := template.NewPostgresTemplateRepository(db)
	tmpl := &template.Template{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		Name:           "Logo Layout",
		Definition:     json.RawMessage(`{"root":{"props":{}},"content":[{"type":"Logo","props":{"id":"logo-1"}}],"zones":{}}`),
	}
	if err := repository.Create(ctx, tmpl); err != nil {
		t.Fatalf("create layout: %v", err)
	}
	if err := repository.SetDefault(ctx, organisationID, tmpl.ID); err != nil {
		t.Fatalf("set default layout: %v", err)
	}
}

// TestInvoicePDF_Logo_DraftLiveIssuedSnapshotted_RealPostgres proves the
// logo follows the same rule as every other seller detail: a Draft's
// render uses the organisation's current logo, while a sent invoice keeps
// the logo it was sent with even after the logo is replaced or removed.
func TestInvoicePDF_Logo_DraftLiveIssuedSnapshotted_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	t.Cleanup(func() {
		_, _ = db.Exec(ctx, "UPDATE organisations SET logo_id = NULL WHERE id = $1", organisationID)
		_, _ = db.Exec(ctx, "DELETE FROM organisation_logos WHERE organisation_id = $1", organisationID)
	})
	customerID := createTestCustomer(t, db, organisationID)
	createTestSettings(t, db, organisationID)
	useCustomTestLayout(t, db, organisationID)

	original := uploadTestLogo(t, db, organisationID)

	sentID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)
	if _, err := newPaymentTestService(db).Send(ctx, organisationID, sentID); err != nil {
		t.Fatalf("send: %v", err)
	}
	draftID := createTestInvoiceWithTotal(t, db, organisationID, customerID, 10000, InvoiceStatusDraft)

	replacement := uploadTestLogo(t, db, organisationID)

	htmlRenderer := &fakeHTMLRenderer{}
	pdfService := NewInvoicePDFService(
		NewPostgresInvoiceRepository(db),
		NewPostgresPaymentRepository(db),
		admin.NewPostgresOrganisationRepository(db),
		customer.NewPostgresCustomerRepository(db),
		customer.NewPostgresAddressRepository(db),
		admin.NewPostgresSettingsRepository(db),
		template.NewPostgresTemplateRepository(db),
		NewInvoicePDFRenderer(),
		htmlRenderer,
		nil,
	)

	if _, _, err := pdfService.Generate(ctx, organisationID, sentID); err != nil {
		t.Fatalf("generate sent pdf: %v", err)
	}
	if _, _, err := pdfService.Generate(ctx, organisationID, draftID); err != nil {
		t.Fatalf("generate draft pdf: %v", err)
	}

	if len(htmlRenderer.calls) != 2 {
		t.Fatalf("expected 2 custom-layout renders, got %d", len(htmlRenderer.calls))
	}
	if got := htmlRenderer.calls[0].Logo; got != original.DataURL() {
		t.Errorf("expected the sent invoice to keep the logo it was sent with")
	}
	if got := htmlRenderer.calls[1].Logo; got != replacement.DataURL() {
		t.Errorf("expected the draft to use the current logo")
	}

	if err := admin.NewPostgresOrganisationRepository(db).ClearLogo(ctx, organisationID); err != nil {
		t.Fatalf("clear logo: %v", err)
	}
	if _, _, err := pdfService.Generate(ctx, organisationID, draftID); err != nil {
		t.Fatalf("generate draft pdf after removal: %v", err)
	}
	if got := htmlRenderer.calls[2].Logo; got != "" {
		t.Errorf("expected no logo on a draft once the logo is removed, got %d chars", len(got))
	}
}
