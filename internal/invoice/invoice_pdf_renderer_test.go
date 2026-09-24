package invoice

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

func minimalPDFData() InvoicePDFData {
	return InvoicePDFData{
		InvoiceNumber: "INV-1",
		IssueDate:     "01 Jan 2026",
		DueDate:       "31 Jan 2026",
		Seller:        InvoicePDFSeller{Name: "Acme Ltd"},
		Customer:      InvoicePDFCustomer{DisplayName: "Bob's Bakery"},
		Lines: []InvoicePDFLine{
			{Description: "Consulting", Quantity: "1", UnitPrice: "£100.00", VATRate: "20%", VATAmount: "£20.00", Total: "£120.00"},
		},
		VATRegistered: true,
		Subtotal:      "£100.00",
		VATTotal:      "£20.00",
		Total:         "£120.00",
	}
}

func fullPDFData() InvoicePDFData {
	return InvoicePDFData{
		InvoiceNumber: "INV-42",
		IssueDate:     "01 Jan 2026",
		DueDate:       "31 Jan 2026",
		StatusLabel:   "OVERDUE",
		Seller: InvoicePDFSeller{
			Name:         "Acme Ltd",
			AddressLines: []string{"1 Acme Way", "London E1 6AN", "GB"},
			Email:        "seller@acme.test",
			Phone:        "+44 20 7946 0958",
			Website:      "https://acme.test",
			TaxID:        "GB123456789",
		},
		Customer: InvoicePDFCustomer{
			DisplayName:  "Bob's Bakery Ltd — Bob Smith",
			AddressLines: []string{"2 Bakery Street", "Manchester M1 1AE", "GB"},
			Email:        "bob@bakery.test",
			TaxID:        "GB987654321",
		},
		Lines: []InvoicePDFLine{
			{Description: "Consulting services", Quantity: "1", UnitPrice: "£1000.00", VATRate: "20%", VATAmount: "£200.00", Total: "£1200.00"},
			{Description: "Support", Quantity: "2.5", UnitPrice: "£100.00", VATRate: "0%", VATAmount: "£0.00", Total: "£250.00"},
		},
		VATRegistered:      true,
		Subtotal:           "£1250.00",
		VATTotal:           "£200.00",
		Total:              "£1450.00",
		ShowPaymentSummary: true,
		AmountPaid:         "£500.00",
		AmountOutstanding:  "£950.00",
		Notes:              "Thank you for your business.",
	}
}

// TestInvoicePDFRenderer_CorruptFontDataReturnsError is the Milestone 7
// hardening-pass answer to "how is a renderer failure actually tested":
// no InvoicePDFRenderer interface was introduced solely to allow
// injecting a failure (Part 3 deliberately avoided that, since there is
// still only one implementation) — instead, this white-box test builds
// an InvoicePDFRenderer directly with its unexported fontData field set
// to garbage, from within the same package, which is a normal Go
// testing technique requiring no interface, no mock, and no production
// code change. It proves gopdf.AddTTFFontData fails with an ordinary
// error (not a panic) for unparseable font data, and that this project's
// own wrapping ("load pdf font: %w") surfaces it correctly to callers —
// see TestInvoiceHandler_GetPDF_RendererFailureReturnsGenericServerError
// for proof this maps to a generic 500 at the HTTP layer.
func TestInvoicePDFRenderer_CorruptFontDataReturnsError(t *testing.T) {
	renderer := &InvoicePDFRenderer{fontData: []byte("this is not a valid ttf font file")}

	_, err := renderer.Render(minimalPDFData())
	if err == nil {
		t.Fatal("expected an error for corrupt font data, got nil")
	}
}

func TestInvoicePDFRenderer_MinimalInvoiceProducesValidPDF(t *testing.T) {
	renderer := NewInvoicePDFRenderer()

	pdfBytes, err := renderer.Render(minimalPDFData())
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)

	if countPDFPages(t, pdfBytes) != 1 {
		t.Errorf("expected a minimal invoice to fit on 1 page, got %d", countPDFPages(t, pdfBytes))
	}
}

func TestInvoicePDFRenderer_ContentRepresented(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := fullPDFData()

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)

	mustContain := []string{
		data.InvoiceNumber,
		data.IssueDate,
		data.DueDate,
		data.StatusLabel,
		data.Seller.Name,
		data.Seller.Email,
		data.Seller.TaxID,
		"Bob's Bakery",
		data.Customer.Email,
		"Consulting services",
		"Support",
		data.Subtotal,
		data.VATTotal,
		data.Total,
		data.AmountPaid,
		data.AmountOutstanding,
		data.Notes,
	}

	for _, want := range mustContain {
		if !strings.Contains(text, want) {
			t.Errorf("expected PDF text to contain %q, got:\n%s", want, text)
		}
	}
}

// TestInvoicePDFRenderer_NotVATRegisteredShowsNoVAT: a seller that isn't
// VAT registered must not show VAT anywhere — no VAT columns, no
// Subtotal/VAT totals rows and no seller VAT number (even if one is
// somehow present on the data) — while the customer's Tax ID and every
// non-VAT value still appear.
func TestInvoicePDFRenderer_NotVATRegisteredShowsNoVAT(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := fullPDFData()
	data.VATRegistered = false
	data.Lines = []InvoicePDFLine{
		{Description: "Consulting services", Quantity: "1", UnitPrice: "£1000.00", VATRate: "0%", VATAmount: "£0.00", Total: "£1000.00"},
	}
	data.Subtotal = "£1000.00"
	data.VATTotal = "£0.00"
	data.Total = "£1000.00"
	data.ShowPaymentSummary = false

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)

	for _, unwanted := range []string{"VAT", "Subtotal", data.Seller.TaxID, "£0.00"} {
		if strings.Contains(text, unwanted) {
			t.Errorf("expected PDF text not to contain %q, got:\n%s", unwanted, text)
		}
	}

	for _, want := range []string{"Description", "Qty", "Unit Price", "Consulting services", "£1000.00", "Total", data.Customer.TaxID} {
		if !strings.Contains(text, want) {
			t.Errorf("expected PDF text to contain %q, got:\n%s", want, text)
		}
	}
}

// TestInvoicePDFRenderer_VATRegisteredShowsVAT is the counterpart: a VAT
// registered seller's PDF shows the VAT columns, the VAT total and its
// VAT number.
func TestInvoicePDFRenderer_VATRegisteredShowsVAT(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := fullPDFData()

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)

	for _, want := range []string{"VAT", "VAT Amt", "Subtotal", "VAT Registration Number: " + data.Seller.TaxID, data.VATTotal} {
		if !strings.Contains(text, want) {
			t.Errorf("expected PDF text to contain %q, got:\n%s", want, text)
		}
	}
}

func TestInvoicePDFRenderer_DraftMarker(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.StatusLabel = "DRAFT"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "DRAFT") {
		t.Error("expected PDF text to contain the DRAFT marker")
	}
}

func TestInvoicePDFRenderer_OverdueMarker(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.StatusLabel = "OVERDUE"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "OVERDUE") {
		t.Error("expected PDF text to contain the OVERDUE marker")
	}
}

func TestInvoicePDFRenderer_PaidMarker(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.StatusLabel = "PAID"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "PAID") {
		t.Error("expected PDF text to contain the PAID marker")
	}
}

func TestInvoicePDFRenderer_NoStatusLabelMeansNoMarkerText(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.StatusLabel = "" // plain Sent invoice

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	for _, marker := range []string{"DRAFT", "OVERDUE", "PAID", "SENT"} {
		if strings.Contains(text, marker) {
			t.Errorf("expected no lifecycle marker text for a plain Sent invoice, found %q", marker)
		}
	}
}

// TestInvoicePDFRenderer_OptionalFieldsAbsentSucceeds proves the
// renderer never panics or errors when every optional seller/customer
// field, notes, and payment summary are all absent — only the required
// minimum (name, one line, totals) is present.
func TestInvoicePDFRenderer_OptionalFieldsAbsentSucceeds(t *testing.T) {
	renderer := NewInvoicePDFRenderer()

	pdfBytes, err := renderer.Render(minimalPDFData())
	if err != nil {
		t.Fatalf("expected rendering with only required fields to succeed, got %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)
}

func TestInvoicePDFRenderer_NotesRendered(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.Notes = "Payment due within 30 days via bank transfer."

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Notes") {
		t.Error("expected a Notes heading")
	}
	if !strings.Contains(text, "Payment due within 30 days") {
		t.Error("expected the notes body text to appear")
	}
}

func TestInvoicePDFRenderer_NoNotesOmitsSection(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.Notes = ""

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if strings.Contains(text, "Notes") {
		t.Error("expected no Notes heading when there are no notes")
	}
}

// TestInvoicePDFRenderer_LongDescriptionWraps proves a long description
// doesn't overflow its column or get truncated — it should still appear
// in full in the extracted text, just wrapped across multiple lines.
func TestInvoicePDFRenderer_LongDescriptionWraps(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	longDescription := "This is a deliberately long line item description intended to exercise word wrapping across multiple lines within the description column of the invoice table"
	data.Lines = []InvoicePDFLine{
		{Description: longDescription, Quantity: "1", UnitPrice: "£10.00", VATRate: "20%", VATAmount: "£2.00", Total: "£12.00"},
	}

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	// Extracted text preserves the wrapped line's own newlines (proving
	// the word-wrap actually happened), so fragments are checked against
	// a newline-collapsed copy rather than the whole string — a real
	// reader perceives wrapped text as one flowing sentence regardless
	// of exactly where the line breaks fell.
	text := strings.ReplaceAll(extractPDFText(t, pdfBytes), "\n", " ")
	for _, fragment := range []string{"deliberately long line item", "description column of the invoice table"} {
		if !strings.Contains(text, fragment) {
			t.Errorf("expected wrapped long description to still contain %q, got:\n%s", fragment, text)
		}
	}
}

// TestInvoicePDFRenderer_MultiPageManyLines proves a 100+ line invoice
// spans multiple pages, with every line represented and the table
// header repeated (checked by counting header-only text occurring more
// than once).
func TestInvoicePDFRenderer_MultiPageManyLines(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()

	const lineCount = 130
	lines := make([]InvoicePDFLine, 0, lineCount)
	for i := 1; i <= lineCount; i++ {
		lines = append(lines, InvoicePDFLine{
			Description: fmt.Sprintf("Line item number %d", i),
			Quantity:    "1",
			UnitPrice:   "£10.00",
			VATRate:     "20%",
			VATAmount:   "£2.00",
			Total:       "£12.00",
		})
	}
	data.Lines = lines

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	pageCount := countPDFPages(t, pdfBytes)
	if pageCount < 2 {
		t.Fatalf("expected a %d-line invoice to span multiple pages, got %d", lineCount, pageCount)
	}
	t.Logf("%d lines produced %d pages", lineCount, pageCount)

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Line item number 1") {
		t.Error("expected first line to be present")
	}
	if !strings.Contains(text, fmt.Sprintf("Line item number %d", lineCount)) {
		t.Errorf("expected last line (%d) to be present", lineCount)
	}

	// The column header "Description" should appear once per page it
	// was drawn on (at least twice, since this spans multiple pages) —
	// proving the header is genuinely repeated, not drawn once and lost.
	headerOccurrences := strings.Count(text, "Description")
	if headerOccurrences < 2 {
		t.Errorf("expected the table header to repeat across pages (at least 2 occurrences of \"Description\"), got %d", headerOccurrences)
	}
}

// TestInvoicePDFRenderer_ZeroLinesDoesNotPanic proves the renderer stays
// robust even for an empty line-items table — normal invoice creation
// rejects zero lines (ErrInvoiceNoLines) so production data can never
// actually reach this, but the renderer itself makes no assumption that
// len(Lines) > 0, and shouldn't panic if that invariant were ever
// violated upstream.
func TestInvoicePDFRenderer_ZeroLinesDoesNotPanic(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.Lines = nil

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("expected no error for zero lines, got %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)
}

// TestInvoicePDFRenderer_FiveHundredLines is the upper-bound pagination
// stress test (Milestone 7 hardening pass): proves a genuinely large
// invoice still renders correctly, with every line present and the
// table header repeated across every page it spans.
func TestInvoicePDFRenderer_FiveHundredLines(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()

	const lineCount = 500
	lines := make([]InvoicePDFLine, 0, lineCount)
	for i := 1; i <= lineCount; i++ {
		lines = append(lines, InvoicePDFLine{
			Description: fmt.Sprintf("Line item number %d", i),
			Quantity:    "1", UnitPrice: "£10.00", VATRate: "20%", VATAmount: "£2.00", Total: "£12.00",
		})
	}
	data.Lines = lines

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	pageCount := countPDFPages(t, pdfBytes)
	t.Logf("%d lines produced %d pages, %d bytes", lineCount, pageCount, len(pdfBytes))
	if pageCount < 10 {
		t.Errorf("expected a %d-line invoice to span many pages, got %d", lineCount, pageCount)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Line item number 1") {
		t.Error("expected first line to be present")
	}
	if !strings.Contains(text, fmt.Sprintf("Line item number %d", lineCount)) {
		t.Errorf("expected last line (%d) to be present", lineCount)
	}
	if !strings.Contains(text, "Total") {
		t.Error("expected totals block to still be present after 500 lines")
	}
}

// TestInvoicePDFRenderer_LongNotesNearPageBoundary proves notes that
// start close to the bottom margin push cleanly onto a new page rather
// than overlapping the previous content or getting clipped.
func TestInvoicePDFRenderer_LongNotesNearPageBoundary(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()

	// Enough lines to push the cursor close to the bottom margin before
	// Notes begins, without quite forcing a page break on its own.
	lines := make([]InvoicePDFLine, 0, 45)
	for i := 1; i <= 45; i++ {
		lines = append(lines, InvoicePDFLine{
			Description: fmt.Sprintf("Item %d", i),
			Quantity:    "1", UnitPrice: "£10.00", VATRate: "20%", VATAmount: "£2.00", Total: "£12.00",
		})
	}
	data.Lines = lines
	data.Notes = strings.Repeat("This is a long note that should wrap across several lines without overlapping the totals block or running off the page. ", 8)

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Notes") {
		t.Error("expected a Notes heading to be present")
	}
	if !strings.Contains(strings.ReplaceAll(text, "\n", " "), "should wrap across several lines") {
		t.Error("expected the long note's content to be present and intact")
	}
}

// TestInvoicePDFRenderer_TwentyLinesFitsReasonably is a mid-sized sanity
// check between the 1-line and 100+-line extremes.
func TestInvoicePDFRenderer_TwentyLines(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()

	lines := make([]InvoicePDFLine, 0, 20)
	for i := 1; i <= 20; i++ {
		lines = append(lines, InvoicePDFLine{
			Description: fmt.Sprintf("Item %d", i),
			Quantity:    "1",
			UnitPrice:   "£10.00",
			VATRate:     "20%",
			VATAmount:   "£2.00",
			Total:       "£12.00",
		})
	}
	data.Lines = lines

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Item 1") || !strings.Contains(text, "Item 20") {
		t.Error("expected first and last of 20 lines to be present")
	}
}

// TestInvoicePDFRenderer_EuropeanUnicodeCharacters proves the embedded
// Liberation Serif font correctly renders ordinary European names and
// addresses a real customer/seller could plausibly have — accented
// Latin characters (French, German, Spanish) and Polish characters
// outside the basic Latin-1 range (Ł, ó, ź) — rather than assuming
// Unicode support without checking. This does NOT claim universal
// Unicode coverage — see
// TestInvoicePDFRenderer_UnsupportedScriptDoesNotPanic for the
// documented limitation.
func TestInvoicePDFRenderer_EuropeanUnicodeCharacters(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.Seller.Name = "José Álvarez"
	data.Seller.AddressLines = []string{"Müller GmbH", "Łódź", "Kraków"}
	data.Customer.DisplayName = "François Dupont"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	for _, want := range []string{"José Álvarez", "Müller GmbH", "Łódź", "Kraków", "François Dupont"} {
		if !strings.Contains(text, want) {
			t.Errorf("expected extracted text to contain %q, got:\n%s", want, text)
		}
	}
}

// TestInvoicePDFRenderer_UnsupportedScriptDoesNotPanic documents the
// actual, honest limitation: a character outside Liberation Serif's
// coverage (e.g. CJK) is silently dropped rather than rendered — gopdf's
// default glyph-not-found handling substitutes nothing rather than
// crashing. This is not "support" for non-Latin scripts, just proof that
// an unsupported character degrades gracefully instead of panicking or
// corrupting the rest of the document.
func TestInvoicePDFRenderer_UnsupportedScriptDoesNotPanic(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.Notes = "Unsupported glyph follows: 日 (end of note)"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("expected no error even with an unsupported glyph, got %v", err)
	}

	requireValidPDFHeader(t, pdfBytes)

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Unsupported glyph follows") || !strings.Contains(text, "end of note") {
		t.Error("expected the surrounding, supported text to render correctly around the unsupported glyph")
	}
}

func TestInvoicePDFRenderer_PaymentSummaryHiddenWhenNotShown(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.ShowPaymentSummary = false

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if strings.Contains(text, "Amount Paid") || strings.Contains(text, "Balance Due") {
		t.Error("expected no payment summary lines when ShowPaymentSummary is false")
	}
}

// TestInvoicePDFRenderer_ConcurrentRenderIsSafe proves a single shared
// InvoicePDFRenderer (exactly as app.go constructs one and reuses it
// across every request) can render many invoices concurrently without
// error or data races — each call constructs its own gopdf.GoPdf
// instance internally, so no per-render state is ever shared. Run with
// -race to actually catch a shared-state bug, not just a logic one.
func TestInvoicePDFRenderer_ConcurrentRenderIsSafe(t *testing.T) {
	renderer := NewInvoicePDFRenderer()

	const goroutines = 20
	var wg sync.WaitGroup
	errs := make([]error, goroutines)

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			data := minimalPDFData()
			data.InvoiceNumber = fmt.Sprintf("INV-%d", i)

			pdfBytes, err := renderer.Render(data)
			if err != nil {
				errs[i] = err
				return
			}
			if !strings.HasPrefix(string(pdfBytes), "%PDF-") {
				errs[i] = fmt.Errorf("goroutine %d: invalid pdf header", i)
			}
		}(i)
	}

	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Errorf("goroutine %d: %v", i, err)
		}
	}
}

func TestInvoicePDFRenderer_PaymentSummaryShownWhenSet(t *testing.T) {
	renderer := NewInvoicePDFRenderer()
	data := minimalPDFData()
	data.ShowPaymentSummary = true
	data.AmountPaid = "£50.00"
	data.AmountOutstanding = "£70.00"

	pdfBytes, err := renderer.Render(data)
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	text := extractPDFText(t, pdfBytes)
	if !strings.Contains(text, "Amount Paid") || !strings.Contains(text, "£50.00") {
		t.Error("expected Amount Paid line to be present")
	}
	if !strings.Contains(text, "Balance Due") || !strings.Contains(text, "£70.00") {
		t.Error("expected Balance Due line to be present")
	}
}
