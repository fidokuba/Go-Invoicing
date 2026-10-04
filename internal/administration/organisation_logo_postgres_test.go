package admin

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// testPNG is a valid 1x1 PNG — enough for http.DetectContentType.
var testPNG = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00\x1f\x15\xc4\x89\x00\x00\x00\rIDATx\x9cc\xf8\x0f\x00\x00\x01\x01\x00\x05\x18\xd8N\x00\x00\x00\x00IEND\xaeB`\x82")

// cleanupLogos registers removal of organisationID's logo rows, which
// must go (and logo_id be cleared) before the organisation row itself
// can be deleted by createTestOrganisation's own cleanup.
func cleanupLogos(t *testing.T, db *pgxpool.Pool, organisationID uuid.UUID) {
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = db.Exec(ctx, "UPDATE organisations SET logo_id = NULL WHERE id = $1", organisationID)
		_, _ = db.Exec(ctx, "DELETE FROM organisation_logos WHERE organisation_id = $1", organisationID)
	})
}

// TestOrganisationService_Logo_RoundTrip_RealPostgres proves upload,
// replace and remove against real SQL — including that a replaced
// logo's row is kept (issued invoices may still reference it), and that
// another organisation can't read it.
func TestOrganisationService_Logo_RoundTrip_RealPostgres(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	otherOrganisationID := createTestOrganisation(t, db)
	cleanupLogos(t, db, organisationID)

	repository := NewPostgresOrganisationRepository(db)
	service := NewOrganisationService(repository, NewPostgresSettingsRepository(db))

	if _, err := service.GetCurrentLogo(ctx, organisationID); !errors.Is(err, ErrOrganisationLogoNotFound) {
		t.Fatalf("expected no logo initially, got %v", err)
	}

	first, err := service.SetLogo(ctx, organisationID, testPNG)
	if err != nil {
		t.Fatalf("set logo: %v", err)
	}
	if first.ContentType != "image/png" {
		t.Errorf("expected sniffed content type image/png, got %q", first.ContentType)
	}

	current, err := service.GetCurrentLogo(ctx, organisationID)
	if err != nil || current.ID != first.ID || !bytes.Equal(current.Data, testPNG) {
		t.Fatalf("expected current logo to be the upload, got %+v, %v", current, err)
	}

	second, err := service.SetLogo(ctx, organisationID, testPNG)
	if err != nil {
		t.Fatalf("replace logo: %v", err)
	}
	organisation, err := repository.GetByID(ctx, organisationID)
	if err != nil || organisation.LogoID == nil || *organisation.LogoID != second.ID {
		t.Fatalf("expected logo_id to point at the replacement, got %v", organisation.LogoID)
	}
	if _, err := repository.GetLogo(ctx, organisationID, first.ID); err != nil {
		t.Errorf("expected the replaced logo's row to be kept, got %v", err)
	}
	if _, err := repository.GetLogo(ctx, otherOrganisationID, second.ID); !errors.Is(err, ErrOrganisationLogoNotFound) {
		t.Errorf("expected another organisation to be unable to read the logo, got %v", err)
	}

	if err := service.RemoveLogo(ctx, organisationID); err != nil {
		t.Fatalf("remove logo: %v", err)
	}
	if _, err := service.GetCurrentLogo(ctx, organisationID); !errors.Is(err, ErrOrganisationLogoNotFound) {
		t.Errorf("expected no current logo after removal, got %v", err)
	}
}

func TestOrganisationService_SetLogo_RejectsNonImagesAndOversize(t *testing.T) {
	service := NewOrganisationService(newFakeOrganisationRepository(), nil)

	if _, err := service.SetLogo(context.Background(), uuid.New(), []byte("<svg xmlns='http://www.w3.org/2000/svg'></svg>")); !errors.Is(err, ErrOrganisationLogoInvalid) {
		t.Errorf("expected SVG to be rejected as invalid, got %v", err)
	}
	if _, err := service.SetLogo(context.Background(), uuid.New(), nil); !errors.Is(err, ErrOrganisationLogoInvalid) {
		t.Errorf("expected empty data to be rejected, got %v", err)
	}
	oversized := append(append([]byte{}, testPNG...), make([]byte, MaxOrganisationLogoBytes)...)
	if _, err := service.SetLogo(context.Background(), uuid.New(), oversized); !errors.Is(err, ErrOrganisationLogoTooLarge) {
		t.Errorf("expected oversize logo to be rejected, got %v", err)
	}
}
