package admin

import (
	"errors"
	"net/http"

	"go-invoicing/internal/httpx"
)

// SettingsHandler owns the HTTP-specific concerns for organisation
// settings: decoding requests, calling the service, translating errors
// into status codes, and encoding responses. It holds no SQL and no
// business rules.
type SettingsHandler struct {
	service *SettingsService
}

func NewSettingsHandler(service *SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
}

// GetCurrent handles GET /organisation/settings — a self-resource route
// with no client-supplied organisation ID, open to every authenticated
// role (Milestone 8 Part 3): reading the current invoice-numbering
// currency/prefix/terms doesn't need admin-only protection the way
// changing them does.
func (h *SettingsHandler) GetCurrent(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	settings, err := h.service.Get(r.Context(), identity.OrganisationID)
	if err != nil {
		if errors.Is(err, ErrSettingsNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "settings_not_found", "settings not found")
			return
		}

		httpx.WriteInternalError(w, r, "settings.get_current", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSettingsResponse(settings))
}

// Update handles PATCH /organisation/settings — admin-only, enforced by
// the RequireRole middleware wrapping this route in app.go, the same
// gate PATCH /organisation already uses: invoice-numbering currency and
// terms affect every future invoice the tenant produces, so this isn't
// freely editable by every role.
func (h *SettingsHandler) Update(w http.ResponseWriter, r *http.Request) {
	identity, ok := RequireAuthenticatedUser(w, r)
	if !ok {
		return
	}

	var request UpdateSettingsRequest
	if !httpx.DecodeJSON(w, r, &request) {
		return
	}

	settings, err := h.service.Update(r.Context(), identity.OrganisationID, request)
	if err != nil {
		if errors.Is(err, ErrSettingsNotFound) {
			httpx.WriteError(w, http.StatusNotFound, "settings_not_found", "settings not found")
			return
		}

		if errors.Is(err, ErrSettingsCurrencyRequired) ||
			errors.Is(err, ErrSettingsCurrencyInvalid) ||
			errors.Is(err, ErrSettingsPaymentTermsInvalid) ||
			errors.Is(err, ErrSettingsInvoicePrefixRequired) {
			httpx.WriteError(w, http.StatusBadRequest, httpx.CodeValidationFailed, err.Error())
			return
		}

		httpx.WriteInternalError(w, r, "settings.update", err)
		return
	}

	httpx.WriteJSON(w, http.StatusOK, toSettingsResponse(settings))
}
