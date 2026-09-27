package template

import (
	"errors"
	"net/http"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/httpx"

	"github.com/google/uuid"
)

// TemplateHandler owns the HTTP-specific concerns for invoice templates:
// decoding requests, calling TemplateService, translating errors into
// status codes. It holds no SQL and no business rules — the same
// division of responsibility every other handler in this codebase
// follows.
type TemplateHandler struct {
	service *TemplateService
}

func NewTemplateHandler(service *TemplateService) *TemplateHandler {
	return &TemplateHandler{service: service}
}

// List handles GET /api/v1/templates — every authenticated role may
// view the organisation's saved layouts (selecting one to use is a
// separate, more consequential action than merely seeing what exists).
func (h *TemplateHandler) List(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	templates, err := h.service.List(r.Context(), identity.OrganisationID)
	if err != nil {
		httpx.WriteInternalError(w, r, "template.list", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toTemplateListResponse(templates))
}

// GetByID handles GET /api/v1/templates/{id} — the single-resource fetch
// a client calls before PATCHing, to get the current ETag to send back
// as If-Match, the same GET/PATCH pairing GET+PATCH /organisation/settings
// already uses.
func (h *TemplateHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid template ID")
		return
	}

	t, err := h.service.GetByID(r.Context(), identity.OrganisationID, id)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "template_not_found", "template not found")
			return
		}

		httpx.WriteInternalError(w, r, "template.get_by_id", err)
		return
	}

	setVersionETag(w, t.Version)
	httpx.WriteJSON(w, http.StatusOK, toTemplateResponse(t))
}

// Create handles POST /api/v1/templates ("Save" on a new template).
func (h *TemplateHandler) Create(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request CreateTemplateRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	t, err := h.service.Create(r.Context(), identity.OrganisationID, request.Name, request.Definition)
	if err != nil {
		if errors.Is(err, ErrTemplateNameRequired) || errors.Is(err, ErrTemplateDefinitionTooLarge) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteInternalError(w, r, "template.create", err)
		return
	}

	setVersionETag(w, t.Version)
	httpx.WriteJSON(w, http.StatusCreated, toTemplateResponse(t))
}

// Update handles PATCH /api/v1/templates/{id} ("Save" on an existing
// template) — If-Match required, exactly like PATCH /organisation/settings.
func (h *TemplateHandler) Update(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid template ID")
		return
	}

	expectedVersion, ok := readIfMatchVersion(w, r)
	if !ok {
		return
	}

	var request UpdateTemplateRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	t, err := h.service.Update(r.Context(), identity.OrganisationID, id, request.Name, request.Definition, expectedVersion)
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "template_not_found", "template not found")
			return
		}
		if errors.Is(err, ErrTemplateIsSystemCannotBeDeleted) {
			httpx.WriteError(w, http.StatusConflict, "template_is_system", "the system default template cannot be edited")
			return
		}
		if errors.Is(err, ErrTemplateVersionConflict) {
			httpx.WriteError(w, http.StatusPreconditionFailed, codePreconditionFailed, staleWriteMessage)
			return
		}
		if errors.Is(err, ErrTemplateNameRequired) || errors.Is(err, ErrTemplateDefinitionTooLarge) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteInternalError(w, r, "template.update", err)
		return
	}

	setVersionETag(w, t.Version)
	httpx.WriteJSON(w, http.StatusOK, toTemplateResponse(t))
}

// Delete handles DELETE /api/v1/templates/{id}. The frontend's own
// confirmation dialog ("Are you sure you want to delete the layout? -
// this is irreversible.") happens before this is ever called — this
// handler enforces the one rule that actually matters server-side: the
// system template can never be deleted, checked by TemplateService.Delete
// regardless of what the client did or didn't confirm.
func (h *TemplateHandler) Delete(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid template ID")
		return
	}

	if err := h.service.Delete(r.Context(), identity.OrganisationID, id); err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "template_not_found", "template not found")
			return
		}
		if errors.Is(err, ErrTemplateIsSystemCannotBeDeleted) {
			httpx.WriteError(w, http.StatusConflict, "template_is_system", "the system default template cannot be deleted")
			return
		}

		httpx.WriteInternalError(w, r, "template.delete", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// SetDefault handles POST /api/v1/templates/{id}/default — "Use This
// Layout". No If-Match: flipping which template is active isn't editing
// the template's own content, so there is nothing for a lost-update to
// clobber the way there is for Update.
func (h *TemplateHandler) SetDefault(w http.ResponseWriter, r *http.Request) {
	identity, ok := admin.RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		httpx.WriteError(w, http.StatusBadRequest, httpx.CodeInvalidRequest, "invalid template ID")
		return
	}

	if err := h.service.SetDefault(r.Context(), identity.OrganisationID, id); err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "template_not_found", "template not found")
			return
		}

		httpx.WriteInternalError(w, r, "template.set_default", err)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
