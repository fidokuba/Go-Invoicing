package invoice

// InvoicePDFData is the dedicated rendering value InvoicePDFRenderer
// consumes. It contains only presentation-ready data: every amount,
// quantity, VAT rate and date is already formatted as a display string
// (via FormatMoney/FormatQuantity/FormatVATRate/FormatInvoiceDate) by
// InvoicePDFService before the renderer ever sees it — the renderer
// itself does no money/date math and holds no formatting policy of its
// own.
//
// Deliberately absent: repository types, UUIDs (invoice/customer/
// organisation IDs), DeletedAt, HTTP DTOs, and any authentication
// identity. The renderer has no way to query PostgreSQL, no notion of
// HTTP, and no notion of tenant ownership — this struct is the entire
// boundary between "data that came from somewhere tenant-scoped" and
// "pure layout," and it carries nothing that would let the renderer
// accidentally reach back across that boundary.
type InvoicePDFData struct {
	InvoiceNumber string
	IssueDate     string
	DueDate       string

	// StatusLabel is the single source of truth for the visual lifecycle
	// marker: "DRAFT", "OVERDUE", "PAID", or "" for a plain Sent invoice
	// (Milestone 7 Part 3 explicitly calls for no SENT watermark) — one
	// field rather than three overlapping booleans, since the renderer
	// only ever needs to know what label (if any) to draw, not why.
	StatusLabel string

	Seller   InvoicePDFSeller
	Customer InvoicePDFCustomer

	Lines []InvoicePDFLine

	Subtotal string
	VATTotal string
	Total    string

	// AmountPaid/AmountOutstanding are only meaningful, and only
	// rendered, when ShowPaymentSummary is true — the partially-paid
	// case (see InvoicePDFService for the exact rule). A fully
	// outstanding invoice needs no redundant paid/balance block, and a
	// fully Paid invoice already says so via StatusLabel.
	ShowPaymentSummary bool
	AmountPaid         string
	AmountOutstanding  string

	// Notes is the raw invoice notes text, wrapped by the renderer at
	// render time — never pre-wrapped here, since wrapping depends on
	// the renderer's own page width and font metrics.
	Notes string
}

// InvoicePDFSeller is the seller ("from") party block. AddressLines is
// already split into display-ready lines (street / city,
// state postcode / country) by whichever helper built this — see
// buildAddressLines — so the renderer only ever needs to print each line
// in order, never assemble one itself. Every field is a plain string,
// empty meaning "not present, don't print this line" — the renderer
// checks for blank, not nil, so this struct needs no pointers.
type InvoicePDFSeller struct {
	Name         string
	AddressLines []string
	Email        string
	Phone        string
	Website      string
	TaxID        string
}

// InvoicePDFCustomer is the "Bill To" party block. DisplayName is
// pre-resolved by the data builder from CompanyName/Name (see
// buildCustomerDisplayName) so the renderer never has to decide whether
// showing both would be redundant — by the time this struct exists, that
// decision has already been made.
type InvoicePDFCustomer struct {
	DisplayName  string
	AddressLines []string
	Email        string
	TaxID        string
}

// InvoicePDFLine is one already-formatted invoice line row. Every value
// is a display string — Quantity via FormatQuantity, UnitPrice/
// VATAmount/Total via FormatMoney, VATRate via FormatVATRate — computed
// once by the data builder, not recalculated or reformatted by the
// renderer.
type InvoicePDFLine struct {
	Description string
	Quantity    string
	UnitPrice   string
	VATRate     string
	VATAmount   string
	Total       string
}
