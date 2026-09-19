package admin

// SettingsResponse is the shape returned to clients for GET and PATCH
// /organisation/settings (Milestone 8 Part 3). It deliberately exposes
// only Currency, PaymentTerms and InvoicePrefix — not ID, OrganisationID,
// InvoiceNumber, or the timestamps: InvoiceNumber in particular is an
// internal allocation counter, not a business setting a client edits,
// and none of the rest is something a settings-editing UI needs.
type SettingsResponse struct {
	Currency      string `json:"currency"`
	PaymentTerms  int    `json:"paymentTerms"`
	InvoicePrefix string `json:"invoicePrefix"`
}

// toSettingsResponse maps the internal domain model onto the API's
// response shape.
func toSettingsResponse(s *Settings) SettingsResponse {
	return SettingsResponse{
		Currency:      s.Currency,
		PaymentTerms:  s.PaymentTerms,
		InvoicePrefix: s.InvoicePrefix,
	}
}

// UpdateSettingsRequest is the shape a client PATCHes to
// /organisation/settings. Every field is a pointer so the request can
// distinguish "omitted — leave unchanged" (nil) from "explicitly
// supplied" (non-nil) — the same genuine partial-update convention
// UpdateOrganisationRequest already established. Unlike that type, none
// of these three fields may ever be cleared to a zero-ish value by
// supplying an empty string/zero — Currency and InvoicePrefix are
// business-required strings, and PaymentTerms' valid range already
// includes 0 ("due immediately") as a meaningful, non-blank value — so
// SettingsService.Update validates each supplied field's content, not
// just its presence.
type UpdateSettingsRequest struct {
	Currency      *string `json:"currency"`
	PaymentTerms  *int    `json:"paymentTerms"`
	InvoicePrefix *string `json:"invoicePrefix"`
}
