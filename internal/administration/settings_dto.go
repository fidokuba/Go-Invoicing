package admin

// SettingsResponse is the shape returned to clients for GET and PATCH
// /organisation/settings (Milestone 8 Part 3). It deliberately exposes
// only Currency, PaymentTerms, InvoicePrefix and QuantityUnits — not ID, OrganisationID,
// InvoiceNumber, or the timestamps: InvoiceNumber in particular is an
// internal allocation counter, not a business setting a client edits,
// and none of the rest is something a settings-editing UI needs.
type SettingsResponse struct {
	Currency      string   `json:"currency"`
	PaymentTerms  int      `json:"paymentTerms"`
	InvoicePrefix string   `json:"invoicePrefix"`
	QuantityUnits []string `json:"quantityUnits"`
}

// toSettingsResponse maps the internal domain model onto the API's
// response shape.
func toSettingsResponse(s *Settings) SettingsResponse {
	return SettingsResponse{
		Currency:      s.Currency,
		PaymentTerms:  s.PaymentTerms,
		InvoicePrefix: s.InvoicePrefix,
		QuantityUnits: nonNilUnits(s.QuantityUnits),
	}
}

// nonNilUnits makes an empty list encode as [] rather than null.
func nonNilUnits(units []string) []string {
	if units == nil {
		return []string{}
	}
	return units
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

	// QuantityUnits, when supplied, replaces the whole list (an empty
	// list is allowed). Each unit is trimmed and must be non-blank, at
	// most MaxQuantityUnitLength characters, and not repeat another
	// (ignoring case).
	QuantityUnits *[]string `json:"quantityUnits"`
}
