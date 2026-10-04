package admin

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"go-invoicing/internal/httpx"
)

// MaxOrganisationLogoBytes caps an uploaded logo's decoded size. Base64
// (how the image travels inside the JSON upload body) grows it by about
// a third, which keeps the largest upload well inside
// httpx.MaxRequestBodyBytes — and the same image is later embedded in
// every custom-layout PDF render request, so it is kept small on purpose.
const MaxOrganisationLogoBytes = 1 << 20 // 1 MiB

// allowedLogoContentTypes are the raster formats accepted, identified by
// sniffing the bytes themselves (http.DetectContentType), never by
// trusting a client-declared type. SVG is deliberately excluded: it is a
// document that can carry scripts, not just pixels.
var allowedLogoContentTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/gif":  true,
	"image/webp": true,
}

var (
	ErrOrganisationLogoNotFound = errors.New("organisation has no logo")
	ErrOrganisationLogoInvalid  = errors.New("logo must be a PNG, JPEG, GIF or WebP image")
	ErrOrganisationLogoTooLarge = errors.New("logo must be 1 MB or smaller")
)

// OrganisationLogo is one uploaded logo image. Rows are immutable: a new
// upload creates a new row (see migration 000023 for why old ones are
// kept).
type OrganisationLogo struct {
	ID             uuid.UUID
	OrganisationID uuid.UUID
	ContentType    string
	Data           []byte
	CreatedAt      time.Time
}

// DataURL returns the logo as a data: URL, for embedding directly into
// HTML (the renderer service has no way to fetch it from the API).
func (l *OrganisationLogo) DataURL() string {
	return "data:" + l.ContentType + ";base64," + base64.StdEncoding.EncodeToString(l.Data)
}

// SetLogo validates data (size, then sniffed image type) and stores it
// as the organisation's new current logo.
func (s *OrganisationService) SetLogo(ctx context.Context, organisationID uuid.UUID, data []byte) (*OrganisationLogo, error) {
	if len(data) == 0 {
		return nil, ErrOrganisationLogoInvalid
	}
	if len(data) > MaxOrganisationLogoBytes {
		return nil, ErrOrganisationLogoTooLarge
	}

	contentType := http.DetectContentType(data)
	if !allowedLogoContentTypes[contentType] {
		return nil, ErrOrganisationLogoInvalid
	}

	logo := &OrganisationLogo{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		ContentType:    contentType,
		Data:           data,
	}

	if err := s.repository.SaveLogo(ctx, organisationID, logo); err != nil {
		return nil, err
	}

	return logo, nil
}

// GetCurrentLogo returns the organisation's current logo, or
// ErrOrganisationLogoNotFound when it has none.
func (s *OrganisationService) GetCurrentLogo(ctx context.Context, organisationID uuid.UUID) (*OrganisationLogo, error) {
	organisation, err := s.repository.GetByID(ctx, organisationID)
	if err != nil {
		return nil, err
	}

	if organisation.LogoID == nil {
		return nil, ErrOrganisationLogoNotFound
	}

	return s.repository.GetLogo(ctx, organisationID, *organisation.LogoID)
}

// RemoveLogo clears the organisation's current logo.
func (s *OrganisationService) RemoveLogo(ctx context.Context, organisationID uuid.UUID) error {
	return s.repository.ClearLogo(ctx, organisationID)
}

// UploadOrganisationLogoRequest is the PUT /organisation/logo body: the
// image file's bytes, base64-encoded — JSON like every other request
// this API accepts (see httpx.DecodeJSON), rather than a one-off
// multipart endpoint.
type UploadOrganisationLogoRequest struct {
	Data string `json:"data"`
}

// OrganisationLogoResponse describes a stored logo, without its bytes.
type OrganisationLogoResponse struct {
	ID          string `json:"id"`
	ContentType string `json:"contentType"`
	Size        int    `json:"size"`
	CreatedAt   string `json:"createdAt"`
}

// UploadLogo handles PUT /organisation/logo — Admin only (RequireRole in
// app.go, the same gate as PATCH /organisation). Replaces any existing
// logo; no If-Match, since the logo is its own sub-resource and a
// replacement can't silently lose anyone else's edit to it.
func (h *OrganisationHandler) UploadLogo(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request UploadOrganisationLogoRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	// Tolerate a full data: URL (what a browser FileReader produces) as
	// well as bare base64 — the sniffed bytes decide the type either way.
	encoded := request.Data
	if strings.HasPrefix(encoded, "data:") {
		if comma := strings.IndexByte(encoded, ','); comma >= 0 {
			encoded = encoded[comma+1:]
		}
	}

	data, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, "logo data must be base64-encoded")
		return
	}

	logo, err := h.service.SetLogo(r.Context(), identity.OrganisationID, data)
	if err != nil {
		switch {
		case errors.Is(err, ErrOrganisationLogoInvalid), errors.Is(err, ErrOrganisationLogoTooLarge):
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
		case errors.Is(err, ErrOrganisationNotFound):
			httpx.WriteError(w, http.StatusNotFound, "organisation_not_found", "organisation not found")
		default:
			httpx.WriteInternalError(w, r, "organisation.upload_logo", err)
		}
		return
	}

	httpx.WriteJSON(w, http.StatusOK, OrganisationLogoResponse{
		ID:          logo.ID.String(),
		ContentType: logo.ContentType,
		Size:        len(logo.Data),
		CreatedAt:   logo.CreatedAt.UTC().Format(time.RFC3339),
	})
}

// GetLogo handles GET /organisation/logo — any authenticated role. Returns
// the image bytes themselves (like GET /invoices/{id}/pdf returns a PDF),
// 404 when the organisation has no logo.
func (h *OrganisationHandler) GetLogo(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	logo, err := h.service.GetCurrentLogo(r.Context(), identity.OrganisationID)
	if err != nil {
		switch {
		case errors.Is(err, ErrOrganisationLogoNotFound), errors.Is(err, ErrOrganisationNotFound):
			httpx.WriteError(w, http.StatusNotFound, "logo_not_found", "organisation has no logo")
		default:
			httpx.WriteInternalError(w, r, "organisation.get_logo", err)
		}
		return
	}

	w.Header().Set("Content-Type", logo.ContentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(logo.Data)))
	w.Header().Set("Cache-Control", "private, no-cache")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(logo.Data)
}

// DeleteLogo handles DELETE /organisation/logo — Admin only. Idempotent:
// 204 whether or not there was a logo to remove.
func (h *OrganisationHandler) DeleteLogo(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	if err := h.service.RemoveLogo(r.Context(), identity.OrganisationID); err != nil {
		if errors.Is(err, ErrOrganisationNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "organisation_not_found", "organisation not found")
			return
		}

		httpx.WriteInternalError(w, r, "organisation.delete_logo", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func optionalUUIDString(id *uuid.UUID) *string {
	if id == nil {
		return nil
	}

	s := id.String()
	return &s
}

// errLogoOrganisationMissing wraps the "no live organisation" case of
// SaveLogo/ClearLogo as ErrOrganisationNotFound.
func errLogoOrganisationMissing(operation string) error {
	return fmt.Errorf("%s: %w", operation, ErrOrganisationNotFound)
}
