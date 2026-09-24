package invoice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/metrics"
)

// PDF-specific business-data-incompleteness sentinels (Milestone 7 Part
// 3). These are deliberately separate from Part 2's Send-time
// ErrInvoiceSnapshot* sentinels even though they guard conceptually
// similar facts (seller name, customer name, currency) — Send and PDF
// generation are different operations with different lifecycles, and
// coupling their error identities would make each harder to change
// independently later. Both map to 409 at the HTTP layer, for the same
// reason: the invoice exists and the request is well-formed, but the
// business data needed to produce a reliable document isn't there yet.
var (
	ErrInvoicePDFSellerNameMissing   = errors.New("invoice PDF seller name is unavailable")
	ErrInvoicePDFCustomerNameMissing = errors.New("invoice PDF customer name is unavailable")
	ErrInvoicePDFCurrencyMissing     = errors.New("invoice PDF currency is unavailable")
	ErrInvoicePDFCurrencyInvalid     = errors.New("invoice PDF currency is not a valid 3-letter code")

	// ErrInvoicePDFDataUnavailable is returned when a Draft invoice's
	// live organisation, customer or settings record cannot be loaded at
	// all (as opposed to being loaded but missing a required field) —
	// see ErrInvoiceSnapshotDataUnavailable in invoice_service.go for the
	// identical reasoning applied to Send.
	ErrInvoicePDFDataUnavailable = errors.New("required business data for the invoice PDF is unavailable")
)

// InvoicePDFService loads exactly the data one invoice's PDF needs,
// decides — based on the invoice's own persisted Status — whether that
// data comes from live records or the immutable Part 2 snapshot, builds
// an InvoicePDFData, and hands it to InvoicePDFRenderer. It performs no
// rendering itself and the renderer performs no data loading: the split
// mirrors this project's existing Handler -> Service -> Repository
// layering, just for a read-only, PDF-shaped operation instead of a
// mutation.
//
// PDF generation is read-only and synchronous: no transaction is opened
// (there is no multi-step write to protect), and no Milestone 6
// background worker is involved. For an issued (Sent/Paid) invoice, the
// snapshot already guarantees the historical consistency a transaction
// would otherwise exist to provide; for a Draft preview, reading live
// organisation/customer/address/settings data without a lock is
// acceptable — a Draft is, by definition, still subject to change.
type InvoicePDFService struct {
	invoiceRepository      InvoiceRepository
	paymentRepository      PaymentRepository
	organisationRepository admin.OrganisationRepository
	customerRepository     customer.CustomerRepository
	addressRepository      customer.AddressRepository
	settingsRepository     admin.SettingsRepository
	renderer               *InvoicePDFRenderer
	metrics                *metrics.Metrics
}

// m may be nil (metrics disabled — see config.Config.MetricsEnabled):
// every *metrics.Metrics method Generate calls below is a nil-safe no-op.
func NewInvoicePDFService(
	invoiceRepository InvoiceRepository,
	paymentRepository PaymentRepository,
	organisationRepository admin.OrganisationRepository,
	customerRepository customer.CustomerRepository,
	addressRepository customer.AddressRepository,
	settingsRepository admin.SettingsRepository,
	renderer *InvoicePDFRenderer,
	m *metrics.Metrics,
) *InvoicePDFService {
	return &InvoicePDFService{
		invoiceRepository:      invoiceRepository,
		paymentRepository:      paymentRepository,
		organisationRepository: organisationRepository,
		customerRepository:     customerRepository,
		addressRepository:      addressRepository,
		settingsRepository:     settingsRepository,
		renderer:               renderer,
		metrics:                m,
	}
}

// Generate produces the PDF bytes for one invoice. organisationID is the
// caller's trusted tenant identity (ultimately AuthenticatedUser
// .OrganisationID) and is the sole value used to scope every read below —
// never a value read off the invoice row or any loaded record. A
// cross-tenant invoiceID fails at this very first call, before any party
// data is ever touched, exactly like every other tenant-scoped operation
// in this project.
// The returned invoiceNumber lets the HTTP handler build a
// Content-Disposition filename without a second, redundant invoice
// lookup — BuildData already fetched the invoice once to assemble data.
//
// This is the Milestone 10 Part 4 PDF-metrics boundary: it measures
// PDF-specific work (data assembly plus rendering) only, not the HTTP
// request GetPDF serves it from — http_request_duration_seconds already
// covers that endpoint's full latency (auth, routing, header/body
// writes included), and a second metric that just re-measured the same
// span wouldn't answer any question the first doesn't already answer.
// result is always the fixed two-value "success"/"error" enum, never
// err.Error() or anything derived from the invoice/customer/organisation
// data BuildData loaded — see RecordPDFGeneration's own doc comment.
func (s *InvoicePDFService) Generate(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (pdfBytes []byte, invoiceNumber string, err error) {
	start := time.Now()
	defer func() {
		result := "success"
		if err != nil {
			result = "error"
		}
		s.metrics.RecordPDFGeneration(result, time.Since(start))
	}()

	data, err := s.BuildData(ctx, organisationID, invoiceID)
	if err != nil {
		return nil, "", err
	}

	pdfBytes, err = s.renderer.Render(data)
	if err != nil {
		return nil, "", fmt.Errorf("render invoice pdf: %w", err)
	}

	return pdfBytes, data.InvoiceNumber, nil
}

// BuildData assembles the InvoicePDFData for one invoice without
// rendering it — every tenant-scoped read, the Draft-vs-issued
// party-data decision, and all business-data validation happen here.
// Generate is a thin wrapper (BuildData + Render); BuildData is exported
// separately so service-level tests can assert directly on the
// assembled data (which repositories were and weren't called, which
// party data ended up in the result) without needing to render a PDF
// and re-extract its text just to check a field.
func (s *InvoicePDFService) BuildData(
	ctx context.Context,
	organisationID uuid.UUID,
	invoiceID uuid.UUID,
) (InvoicePDFData, error) {
	inv, err := s.invoiceRepository.GetByID(ctx, organisationID, invoiceID)
	if err != nil {
		return InvoicePDFData{}, err
	}

	lines, err := s.invoiceRepository.GetLinesByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return InvoicePDFData{}, fmt.Errorf("get invoice lines for pdf: %w", err)
	}

	amountPaid, err := s.paymentRepository.GetTotalPaidByInvoiceID(ctx, organisationID, invoiceID)
	if err != nil {
		return InvoicePDFData{}, fmt.Errorf("get amount paid for pdf: %w", err)
	}

	var (
		seller   InvoicePDFSeller
		cust     InvoicePDFCustomer
		currency string
	)

	// The critical party-data rule (Milestone 7 Part 3 section 5): a
	// Draft invoice has no snapshot yet and uses current live data; any
	// persisted Sent or Paid invoice — including one that's currently
	// displayed as effectively Overdue, which is still persisted Sent —
	// uses ONLY the immutable snapshot. This branch is the one place in
	// the whole service that decides which data source is used; nothing
	// downstream ever mixes the two.
	if inv.Status == InvoiceStatusDraft {
		seller, cust, currency, err = s.buildLivePartyData(ctx, organisationID, inv.CustomerID)
	} else {
		seller, cust, currency, err = s.buildSnapshotPartyData(inv)
	}
	if err != nil {
		return InvoicePDFData{}, err
	}

	return buildInvoicePDFData(inv, lines, amountPaid, seller, cust, currency, time.Now().UTC()), nil
}

// buildLivePartyData loads the organisation, the invoice's customer, the
// customer's billing address (optional), and settings currency for a
// Draft invoice's preview — all tenant-scoped by organisationID. A
// missing billing address (customer.ErrBillingAddressNotFound) is
// expected and harmless; every other lookup failure is surfaced as
// ErrInvoicePDFDataUnavailable, and a resolvable-but-empty required
// field (blank organisation name, blank customer name, missing/invalid
// currency) is rejected explicitly rather than silently producing a
// misleading PDF.
func (s *InvoicePDFService) buildLivePartyData(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
) (InvoicePDFSeller, InvoicePDFCustomer, string, error) {
	organisation, err := s.organisationRepository.GetByID(ctx, organisationID)
	if err != nil {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", fmt.Errorf("%w: look up organisation: %v", ErrInvoicePDFDataUnavailable, err)
	}

	if strings.TrimSpace(organisation.Name) == "" {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFSellerNameMissing
	}

	cust, err := s.customerRepository.GetByID(ctx, organisationID, customerID)
	if err != nil {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", fmt.Errorf("%w: look up customer: %v", ErrInvoicePDFDataUnavailable, err)
	}

	if strings.TrimSpace(cust.Name) == "" {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCustomerNameMissing
	}

	settings, err := s.settingsRepository.GetByOrganisationID(ctx, organisationID)
	if err != nil {
		if errors.Is(err, admin.ErrSettingsNotFound) {
			return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCurrencyMissing
		}

		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", fmt.Errorf("%w: look up settings: %v", ErrInvoicePDFDataUnavailable, err)
	}

	currency := normalizeCurrency(settings.Currency)
	if !isValidCurrencyCode(currency) {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCurrencyInvalid
	}

	seller := InvoicePDFSeller{
		Name:         organisation.Name,
		AddressLines: buildAddressLines(organisation.Address, organisation.City, organisation.State, organisation.PostalCode, organisation.Country),
		Email:        trimmedOrEmpty(organisation.Email),
		Phone:        trimmedOrEmpty(organisation.Phone),
		Website:      trimmedOrEmpty(organisation.Website),
		TaxID:        trimmedOrEmpty(sellerVATNumber(organisation)),
	}

	var addressLines []string
	billingAddress, err := s.addressRepository.GetBillingAddressByCustomerID(ctx, organisationID, customerID)
	if err != nil {
		if !errors.Is(err, customer.ErrBillingAddressNotFound) {
			return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", fmt.Errorf("%w: look up billing address: %v", ErrInvoicePDFDataUnavailable, err)
		}
		// No billing address yet — acceptable; addressLines stays nil.
	} else {
		addressLines = buildAddressLines(
			strPtrIfNotEmpty(billingAddress.Street),
			strPtrIfNotEmpty(billingAddress.City),
			strPtrIfNotEmpty(billingAddress.State),
			strPtrIfNotEmpty(billingAddress.PostalCode),
			strPtrIfNotEmpty(billingAddress.Country),
		)
	}

	custData := InvoicePDFCustomer{
		DisplayName:  buildCustomerDisplayName(cust.Name, trimmedOrEmpty(cust.CompanyName)),
		AddressLines: addressLines,
		Email:        trimmedOrEmpty(cust.Email),
		TaxID:        trimmedOrEmpty(cust.TaxID),
	}

	return seller, custData, currency, nil
}

// buildSnapshotPartyData builds the seller/customer/currency data for an
// issued (Sent or Paid) invoice exclusively from the immutable snapshot
// already stored on inv — it makes no repository calls at all, which is
// exactly what proves live organisation/customer/address/settings
// changes can never affect an issued invoice's PDF. A snapshot missing
// any of the three required fields (which should only happen for an
// invoice sent before Milestone 7 Part 2 existed) fails clearly rather
// than falling back to live data, which would silently destroy the
// historical guarantee Part 2 exists to provide.
func (s *InvoicePDFService) buildSnapshotPartyData(inv *Invoice) (InvoicePDFSeller, InvoicePDFCustomer, string, error) {
	if inv.SellerName == nil || strings.TrimSpace(*inv.SellerName) == "" {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFSellerNameMissing
	}

	if inv.CustomerName == nil || strings.TrimSpace(*inv.CustomerName) == "" {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCustomerNameMissing
	}

	if inv.Currency == nil || strings.TrimSpace(*inv.Currency) == "" {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCurrencyMissing
	}

	currency := normalizeCurrency(*inv.Currency)
	if !isValidCurrencyCode(currency) {
		return InvoicePDFSeller{}, InvoicePDFCustomer{}, "", ErrInvoicePDFCurrencyInvalid
	}

	seller := InvoicePDFSeller{
		Name:         *inv.SellerName,
		AddressLines: buildAddressLines(inv.SellerAddress, inv.SellerCity, inv.SellerState, inv.SellerPostalCode, inv.SellerCountry),
		Email:        trimmedOrEmpty(inv.SellerEmail),
		Phone:        trimmedOrEmpty(inv.SellerPhone),
		Website:      trimmedOrEmpty(inv.SellerWebsite),
		TaxID:        trimmedOrEmpty(inv.SellerTaxID),
	}

	cust := InvoicePDFCustomer{
		DisplayName:  buildCustomerDisplayName(*inv.CustomerName, trimmedOrEmpty(inv.CustomerCompanyName)),
		AddressLines: buildAddressLines(inv.CustomerAddress, inv.CustomerCity, inv.CustomerState, inv.CustomerPostalCode, inv.CustomerCountry),
		Email:        trimmedOrEmpty(inv.CustomerEmail),
		TaxID:        trimmedOrEmpty(inv.CustomerTaxID),
	}

	return seller, cust, currency, nil
}

// trimmedOrEmpty returns the trimmed value of an optional string field,
// or "" if it's nil — the single place InvoicePDFSeller/Customer field
// construction converts this project's *string "nullable" convention
// into the plain, always-safe-to-print strings InvoicePDFData uses.
func trimmedOrEmpty(value *string) string {
	if value == nil {
		return ""
	}

	return strings.TrimSpace(*value)
}

// strPtrIfNotEmpty is the inverse of trimmedOrEmpty, needed only because
// customer.Address stores Street/City/State/PostalCode/Country as plain
// strings (see Address's own doc comment) while buildAddressLines takes
// *string to match Organisation/the invoice snapshot's nullable
// convention — this lets one buildAddressLines implementation serve both
// shapes without duplicating its logic.
func strPtrIfNotEmpty(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}

// buildAddressLines assembles display-ready address lines from optional
// components, omitting anything absent entirely rather than printing an
// empty line or a label with nothing after it. City and State combine
// onto one line with the postal code appended, so a sparse address (e.g.
// city + postcode only, no street) still reads naturally instead of
// leaving visibly empty lines.
func buildAddressLines(street, city, state, postalCode, country *string) []string {
	var lines []string

	if s := trimmedOrEmpty(street); s != "" {
		lines = append(lines, s)
	}

	cityLine := trimmedOrEmpty(city)
	if st := trimmedOrEmpty(state); st != "" {
		if cityLine != "" {
			cityLine += ", " + st
		} else {
			cityLine = st
		}
	}
	if pc := trimmedOrEmpty(postalCode); pc != "" {
		if cityLine != "" {
			cityLine += " " + pc
		} else {
			cityLine = pc
		}
	}
	if cityLine != "" {
		lines = append(lines, cityLine)
	}

	if c := trimmedOrEmpty(country); c != "" {
		lines = append(lines, c)
	}

	return lines
}

// buildCustomerDisplayName resolves the single display name shown in the
// "Bill To" block, avoiding awkward duplication (Milestone 7 Part 3
// section 18): a blank or identical companyName is dropped entirely; a
// genuinely different one is shown alongside name rather than replacing
// it, since both are real, useful facts about who the invoice is billed
// to.
func buildCustomerDisplayName(name, companyName string) string {
	name = strings.TrimSpace(name)
	companyName = strings.TrimSpace(companyName)

	if companyName == "" || strings.EqualFold(companyName, name) {
		return name
	}

	return companyName + " — " + name
}

// buildInvoicePDFData assembles the final InvoicePDFData from the
// already-loaded invoice, its lines, the amount already paid, and the
// party/currency data buildLivePartyData or buildSnapshotPartyData
// produced. now drives EffectiveStatus for the StatusLabel exactly the
// way toInvoiceResponse drives it for the JSON API's Status field — a
// PDF render is a read, the same as an HTTP GET, so it derives Overdue
// the same way.
func buildInvoicePDFData(
	inv *Invoice,
	lines []*Line,
	amountPaid int64,
	seller InvoicePDFSeller,
	cust InvoicePDFCustomer,
	currency string,
	now time.Time,
) InvoicePDFData {
	pdfLines := make([]InvoicePDFLine, 0, len(lines))
	for _, l := range lines {
		pdfLines = append(pdfLines, InvoicePDFLine{
			Description: l.Description,
			Quantity:    FormatQuantity(l.Quantity),
			UnitPrice:   FormatMoney(l.UnitPrice, currency),
			VATRate:     FormatVATRate(l.VATRate),
			VATAmount:   FormatMoney(l.VATAmount, currency),
			Total:       FormatMoney(l.Total, currency),
		})
	}

	data := InvoicePDFData{
		InvoiceNumber: inv.InvoiceNumber,
		IssueDate:     FormatInvoiceDate(inv.IssueDate),
		DueDate:       FormatInvoiceDate(inv.DueDate),
		StatusLabel:   statusLabelFor(inv.EffectiveStatus(now)),
		Seller:        seller,
		Customer:      cust,
		Lines:         pdfLines,
		VATRegistered: inv.VATRegistered,
		Subtotal:      FormatMoney(inv.Subtotal, currency),
		VATTotal:      FormatMoney(inv.VATTotal, currency),
		Total:         FormatMoney(inv.Total, currency),
	}

	// Partially paid only — see InvoicePDFData's own doc comment for why
	// fully-outstanding and fully-Paid invoices both omit this block.
	if amountPaid > 0 && amountPaid < inv.Total {
		data.ShowPaymentSummary = true
		data.AmountPaid = FormatMoney(amountPaid, currency)
		data.AmountOutstanding = FormatMoney(inv.Total-amountPaid, currency)
	}

	if inv.Notes != nil {
		data.Notes = strings.TrimSpace(*inv.Notes)
	}

	return data
}

// statusLabelFor maps an effective status onto the PDF's visual marker.
// Sent (the "nothing unusual" case) deliberately maps to "" — no
// watermark — per Milestone 7 Part 3's explicit instruction not to stamp
// ordinary Sent invoices.
func statusLabelFor(effectiveStatus string) string {
	switch effectiveStatus {
	case InvoiceStatusDraft:
		return "DRAFT"
	case InvoiceStatusOverdue:
		return "OVERDUE"
	case InvoiceStatusPaid:
		return "PAID"
	default:
		return ""
	}
}
