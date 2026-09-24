package invoice

import (
	"fmt"
	"time"

	"github.com/signintech/gopdf"
)

// Page geometry. A4 in points (gopdf's own gopdf.PageSizeA4 is 595x842),
// spelled out explicitly here rather than referencing that package
// variable, so every layout constant below is visibly derived from the
// same numbers.
const (
	pdfFontFamily = "LiberationSerif"

	pdfPageWidth  = 595.0
	pdfPageHeight = 842.0

	pdfMarginLeft   = 40.0
	pdfMarginTop    = 40.0
	pdfMarginRight  = 40.0
	pdfMarginBottom = 50.0

	pdfContentWidth = pdfPageWidth - pdfMarginLeft - pdfMarginRight

	pdfFontSizeTitle   = 20.0
	pdfFontSizeHeading = 11.0
	pdfFontSizeBody    = 9.0

	pdfLineHeight  = 12.0
	pdfCellPadding = 3.0
)

// Line-items table column geometry. Widths sum to exactly
// pdfContentWidth (195+45+70+40+75+90 = 515).
const (
	pdfColDescX = pdfMarginLeft
	pdfColDescW = 195.0

	pdfColQtyW = 45.0

	pdfColPriceW = 70.0

	pdfColVATRateW = 40.0

	pdfColVATAmountW = 75.0

	pdfColTotalW = pdfContentWidth - pdfColDescW - pdfColQtyW - pdfColPriceW - pdfColVATRateW - pdfColVATAmountW
)

var (
	pdfColQtyX       = pdfColDescX + pdfColDescW
	pdfColPriceX     = pdfColQtyX + pdfColQtyW
	pdfColVATRateX   = pdfColPriceX + pdfColPriceW
	pdfColVATAmountX = pdfColVATRateX + pdfColVATRateW
	pdfColTotalX     = pdfColVATAmountX + pdfColVATAmountW
)

// lineColumns is the line-items table geometry for one render. With VAT
// shown it is exactly the constants above. Without VAT (a non-registered
// seller) the two VAT columns are dropped and their width goes to
// Description, shifting Qty and Unit Price right; Total never moves.
type lineColumns struct {
	showVAT bool
	descW   float64
	qtyX    float64
	priceX  float64
}

func newLineColumns(showVAT bool) lineColumns {
	extra := 0.0
	if !showVAT {
		extra = pdfColVATRateW + pdfColVATAmountW
	}

	return lineColumns{
		showVAT: showVAT,
		descW:   pdfColDescW + extra,
		qtyX:    pdfColQtyX + extra,
		priceX:  pdfColPriceX + extra,
	}
}

// InvoicePDFRenderer turns an already-assembled InvoicePDFData into PDF
// bytes. It performs layout only — no PostgreSQL access, no HTTP
// awareness, no authentication or tenant-ownership concerns. Every value
// it draws arrives already resolved and formatted on data; this type
// holds no formatting policy of its own.
//
// It carries no mutable per-render state (only the embedded font bytes,
// which never change), so a single InvoicePDFRenderer is safe to reuse
// concurrently across requests — Render constructs a fresh gopdf.GoPdf
// for every call, never touching shared state.
type InvoicePDFRenderer struct {
	fontData []byte
}

// NewInvoicePDFRenderer constructs a renderer using the embedded
// Liberation Serif font (see pdf_assets.go) — no filesystem font lookup.
func NewInvoicePDFRenderer() *InvoicePDFRenderer {
	return &InvoicePDFRenderer{fontData: invoicePDFFont}
}

// Render lays out data onto a fresh PDF document and returns the
// resulting bytes. It never writes directly to an http.ResponseWriter —
// returning []byte is what lets the same renderer eventually serve a
// future email-attachment feature without any change here (Milestone 7
// Part 3 section 24) — Part 3 itself only wires this into an HTTP
// download.
func (r *InvoicePDFRenderer) Render(data InvoicePDFData) ([]byte, error) {
	pdf := &gopdf.GoPdf{}
	pdf.Start(gopdf.Config{PageSize: gopdf.Rect{W: pdfPageWidth, H: pdfPageHeight}})
	pdf.SetMargins(pdfMarginLeft, pdfMarginTop, pdfMarginRight, pdfMarginBottom)

	if err := pdf.AddTTFFontData(pdfFontFamily, r.fontData); err != nil {
		return nil, fmt.Errorf("load pdf font: %w", err)
	}

	pdf.SetInfo(gopdf.PdfInfo{
		Title:        "Invoice " + data.InvoiceNumber,
		Author:       data.Seller.Name,
		CreationDate: time.Now(),
	})

	c := &pdfCanvas{pdf: pdf, cols: newLineColumns(data.VATRegistered)}
	c.addPage()

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return nil, err
	}

	if err := c.drawHeader(data); err != nil {
		return nil, fmt.Errorf("draw pdf header: %w", err)
	}

	if err := c.drawCustomerBlock(data.Customer); err != nil {
		return nil, fmt.Errorf("draw pdf customer block: %w", err)
	}

	if err := c.drawLineItemsTable(data.Lines); err != nil {
		return nil, fmt.Errorf("draw pdf line items: %w", err)
	}

	if err := c.drawTotals(data); err != nil {
		return nil, fmt.Errorf("draw pdf totals: %w", err)
	}

	if data.Notes != "" {
		if err := c.drawNotes(data.Notes); err != nil {
			return nil, fmt.Errorf("draw pdf notes: %w", err)
		}
	}

	pdfBytes, err := pdf.GetBytesPdfReturnErr()
	if err != nil {
		return nil, fmt.Errorf("encode pdf: %w", err)
	}

	return pdfBytes, nil
}

// pdfCanvas wraps a single render's *gopdf.GoPdf with this layout's own
// pagination and drawing helpers. It exists purely to avoid repeating
// the same page/margin arithmetic in every drawing method — it is not a
// general drawing abstraction, and nothing about it survives past one
// Render call.
type pdfCanvas struct {
	pdf  *gopdf.GoPdf
	cols lineColumns
}

func (c *pdfCanvas) addPage() {
	c.pdf.AddPage()
}

func (c *pdfCanvas) setFont(size float64) error {
	return c.pdf.SetFont(pdfFontFamily, "", size)
}

// remainingHeight reports how much vertical space is left on the current
// page above the bottom margin.
func (c *pdfCanvas) remainingHeight() float64 {
	return (pdfPageHeight - pdfMarginBottom) - c.pdf.GetY()
}

// ensureSpace starts a new page — resetting Y to the top margin, exactly
// as AddPage already does — if height would not fit in the remaining
// space on the current page. Callers that need the table header redrawn
// after a page break check the return value.
func (c *pdfCanvas) ensureSpace(height float64) (newPage bool) {
	if c.remainingHeight() < height {
		c.addPage()
		return true
	}

	return false
}

// cell draws single-line, left-aligned text at (x, y) — a thin wrapper
// so every drawing method below shares one upper-left-corner mental
// model (gopdf.GoPdf.Text uses the baseline instead, which this
// deliberately avoids mixing in).
func (c *pdfCanvas) cell(x, y, width float64, text string) error {
	c.pdf.SetXY(x, y)
	return c.pdf.Cell(&gopdf.Rect{W: width, H: pdfLineHeight}, text)
}

// cellRight draws single-line, right-aligned text within a column of
// width starting at x.
func (c *pdfCanvas) cellRight(x, y, width float64, text string) error {
	c.pdf.SetXY(x, y)
	return c.pdf.CellWithOption(&gopdf.Rect{W: width, H: pdfLineHeight}, text, gopdf.CellOption{Align: gopdf.Right})
}

// drawHeader renders the seller block (left) and the "INVOICE" title
// plus invoice number/dates (right) side by side, followed by the
// lifecycle status marker if one applies.
func (c *pdfCanvas) drawHeader(data InvoicePDFData) error {
	startY := c.pdf.GetY()
	rightColX := pdfMarginLeft + pdfContentWidth*0.55
	rightColW := pdfContentWidth * 0.45

	// Seller (left column).
	y := startY
	if err := c.setFont(pdfFontSizeHeading); err != nil {
		return err
	}
	if err := c.cell(pdfMarginLeft, y, pdfContentWidth*0.5, data.Seller.Name); err != nil {
		return err
	}
	y += pdfLineHeight + 4

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return err
	}
	for _, line := range data.Seller.AddressLines {
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth*0.5, line); err != nil {
			return err
		}
		y += pdfLineHeight
	}
	for _, contact := range []string{data.Seller.Email, data.Seller.Phone, data.Seller.Website} {
		if contact == "" {
			continue
		}
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth*0.5, contact); err != nil {
			return err
		}
		y += pdfLineHeight
	}
	if data.VATRegistered && data.Seller.TaxID != "" {
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth*0.5, "VAT Registration Number: "+data.Seller.TaxID); err != nil {
			return err
		}
		y += pdfLineHeight
	}
	sellerBottom := y

	// Invoice title/metadata (right column).
	y = startY
	if err := c.setFont(pdfFontSizeTitle); err != nil {
		return err
	}
	if err := c.cellRight(rightColX, y, rightColW, "INVOICE"); err != nil {
		return err
	}
	y += pdfFontSizeTitle + 8

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return err
	}
	metadataRows := []struct{ label, value string }{
		{"Invoice #:", data.InvoiceNumber},
		{"Issue Date:", data.IssueDate},
		{"Due Date:", data.DueDate},
	}
	for _, row := range metadataRows {
		if err := c.cellRight(rightColX, y, rightColW, row.label+" "+row.value); err != nil {
			return err
		}
		y += pdfLineHeight
	}

	if data.StatusLabel != "" {
		y += 6
		if err := c.drawStatusMarker(rightColX, y, rightColW, data.StatusLabel); err != nil {
			return err
		}
		y += 24
	}

	metadataBottom := y

	bottom := sellerBottom
	if metadataBottom > bottom {
		bottom = metadataBottom
	}

	c.pdf.SetXY(pdfMarginLeft, bottom+12)
	return c.setFont(pdfFontSizeBody)
}

// statusMarkerColors gives each lifecycle marker a distinct, legible
// background — muted rather than alarming, since Overdue/unpaid is a
// normal business state, not an error.
var statusMarkerColors = map[string][3]uint8{
	"DRAFT":   {120, 120, 120},
	"OVERDUE": {176, 48, 48},
	"PAID":    {40, 120, 60},
}

// drawStatusMarker draws a small filled, right-aligned badge containing
// label — the sole visual lifecycle indicator on the document (Milestone
// 7 Part 3 section 10). An ordinary Sent invoice never calls this at all
// (see statusLabelFor), so there is no risk of an unwanted watermark.
func (c *pdfCanvas) drawStatusMarker(x, y, width float64, label string) error {
	color, ok := statusMarkerColors[label]
	if !ok {
		color = [3]uint8{90, 90, 90}
	}

	badgeWidth := 90.0
	badgeX := x + width - badgeWidth
	badgeHeight := 20.0

	c.pdf.SetFillColor(color[0], color[1], color[2])
	c.pdf.RectFromUpperLeftWithStyle(badgeX, y, badgeWidth, badgeHeight, "F")

	if err := c.setFont(pdfFontSizeHeading); err != nil {
		return err
	}
	c.pdf.SetTextColor(255, 255, 255)

	c.pdf.SetXY(badgeX, y+4)
	if err := c.pdf.CellWithOption(&gopdf.Rect{W: badgeWidth, H: badgeHeight}, label, gopdf.CellOption{Align: gopdf.Center}); err != nil {
		return err
	}

	c.pdf.SetTextColor(0, 0, 0)
	return c.setFont(pdfFontSizeBody)
}

// drawCustomerBlock renders the "Bill To" heading and customer details.
func (c *pdfCanvas) drawCustomerBlock(cust InvoicePDFCustomer) error {
	y := c.pdf.GetY()

	if err := c.setFont(pdfFontSizeHeading); err != nil {
		return err
	}
	if err := c.cell(pdfMarginLeft, y, pdfContentWidth, "BILL TO"); err != nil {
		return err
	}
	y += pdfLineHeight + 4

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return err
	}
	if err := c.cell(pdfMarginLeft, y, pdfContentWidth, cust.DisplayName); err != nil {
		return err
	}
	y += pdfLineHeight

	for _, line := range cust.AddressLines {
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth, line); err != nil {
			return err
		}
		y += pdfLineHeight
	}

	if cust.Email != "" {
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth, cust.Email); err != nil {
			return err
		}
		y += pdfLineHeight
	}

	if cust.TaxID != "" {
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth, "Tax ID: "+cust.TaxID); err != nil {
			return err
		}
		y += pdfLineHeight
	}

	c.pdf.SetXY(pdfMarginLeft, y+12)
	return nil
}

// drawLineItemsTable renders the table header followed by every line,
// handling pagination explicitly: a new page is started whenever a row
// (or the header itself) wouldn't fit in the remaining space, and the
// column header is redrawn at the top of every new page a row continues
// onto (Milestone 7 Part 3 section 20).
func (c *pdfCanvas) drawLineItemsTable(lines []InvoicePDFLine) error {
	const headerHeight = 20.0

	c.ensureSpace(headerHeight + pdfLineHeight + pdfCellPadding*2) // header + at least one row
	if err := c.drawTableHeader(); err != nil {
		return err
	}

	for _, line := range lines {
		if err := c.drawLineRow(line); err != nil {
			return err
		}
	}

	c.pdf.SetXY(pdfMarginLeft, c.pdf.GetY()+10)
	return nil
}

func (c *pdfCanvas) drawTableHeader() error {
	const headerHeight = 20.0

	y := c.pdf.GetY()
	c.pdf.SetFillColor(230, 230, 230)
	c.pdf.RectFromUpperLeftWithStyle(pdfMarginLeft, y, pdfContentWidth, headerHeight, "F")

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return err
	}

	labelY := y + 5
	if err := c.cell(pdfColDescX+pdfCellPadding, labelY, c.cols.descW-pdfCellPadding, "Description"); err != nil {
		return err
	}
	if err := c.cellRight(c.cols.qtyX, labelY, pdfColQtyW-pdfCellPadding, "Qty"); err != nil {
		return err
	}
	if err := c.cellRight(c.cols.priceX, labelY, pdfColPriceW-pdfCellPadding, "Unit Price"); err != nil {
		return err
	}
	if c.cols.showVAT {
		if err := c.cellRight(pdfColVATRateX, labelY, pdfColVATRateW-pdfCellPadding, "VAT"); err != nil {
			return err
		}
		if err := c.cellRight(pdfColVATAmountX, labelY, pdfColVATAmountW-pdfCellPadding, "VAT Amt"); err != nil {
			return err
		}
	}
	if err := c.cellRight(pdfColTotalX, labelY, pdfColTotalW-pdfCellPadding, "Total"); err != nil {
		return err
	}

	c.pdf.SetXY(pdfMarginLeft, y+headerHeight)
	return nil
}

// drawLineRow renders one invoice line, wrapping its description across
// as many lines as needed and sizing the row to match — every other
// column's value is drawn once, aligned to the top of the (possibly
// multi-line) row.
func (c *pdfCanvas) drawLineRow(line InvoicePDFLine) error {
	wrapped, err := c.pdf.SplitTextWithWordWrap(line.Description, c.cols.descW-2*pdfCellPadding)
	if err != nil {
		return fmt.Errorf("wrap line description: %w", err)
	}
	if len(wrapped) == 0 {
		wrapped = []string{""}
	}

	rowHeight := float64(len(wrapped))*pdfLineHeight + pdfCellPadding*2

	if c.ensureSpace(rowHeight) {
		if err := c.drawTableHeader(); err != nil {
			return err
		}
	}

	top := c.pdf.GetY()
	textY := top + pdfCellPadding

	for _, w := range wrapped {
		if err := c.cell(pdfColDescX+pdfCellPadding, textY, c.cols.descW-pdfCellPadding, w); err != nil {
			return err
		}
		textY += pdfLineHeight
	}

	valueY := top + pdfCellPadding
	if err := c.cellRight(c.cols.qtyX, valueY, pdfColQtyW-pdfCellPadding, line.Quantity); err != nil {
		return err
	}
	if err := c.cellRight(c.cols.priceX, valueY, pdfColPriceW-pdfCellPadding, line.UnitPrice); err != nil {
		return err
	}
	if c.cols.showVAT {
		if err := c.cellRight(pdfColVATRateX, valueY, pdfColVATRateW-pdfCellPadding, line.VATRate); err != nil {
			return err
		}
		if err := c.cellRight(pdfColVATAmountX, valueY, pdfColVATAmountW-pdfCellPadding, line.VATAmount); err != nil {
			return err
		}
	}
	if err := c.cellRight(pdfColTotalX, valueY, pdfColTotalW-pdfCellPadding, line.Total); err != nil {
		return err
	}

	bottom := top + rowHeight
	c.pdf.SetLineWidth(0.4)
	c.pdf.SetStrokeColor(210, 210, 210)
	c.pdf.Line(pdfMarginLeft, bottom, pdfMarginLeft+pdfContentWidth, bottom)

	c.pdf.SetXY(pdfMarginLeft, bottom)
	return nil
}

// drawTotals renders the right-aligned Subtotal/VAT/Total block, plus
// Amount Paid/Balance Due when InvoicePDFData.ShowPaymentSummary is set.
// A non-VAT-registered invoice shows Total alone: without VAT, Subtotal
// would only repeat it.
// The whole block is kept together on one page — Milestone 7 Part 3
// explicitly asks that totals "appear together where practical" rather
// than splitting across a page boundary.
func (c *pdfCanvas) drawTotals(data InvoicePDFData) error {
	rows := []struct{ label, value string }{
		{"Total", data.Total},
	}
	if data.VATRegistered {
		rows = []struct{ label, value string }{
			{"Subtotal", data.Subtotal},
			{"VAT", data.VATTotal},
			{"Total", data.Total},
		}
	}
	if data.ShowPaymentSummary {
		rows = append(rows,
			struct{ label, value string }{"Amount Paid", data.AmountPaid},
			struct{ label, value string }{"Balance Due", data.AmountOutstanding},
		)
	}

	blockHeight := float64(len(rows))*pdfLineHeight + pdfCellPadding*2
	c.ensureSpace(blockHeight)

	labelX := pdfMarginLeft + pdfContentWidth - 220
	labelW := 130.0
	valueX := labelX + labelW
	valueW := 90.0

	y := c.pdf.GetY() + pdfCellPadding
	for i, row := range rows {
		if row.label == "Total" {
			if err := c.setFont(pdfFontSizeHeading); err != nil {
				return err
			}
		}

		if err := c.cellRight(labelX, y, labelW-pdfCellPadding, row.label); err != nil {
			return err
		}
		if err := c.cellRight(valueX, y, valueW, row.value); err != nil {
			return err
		}

		if row.label == "Total" {
			if err := c.setFont(pdfFontSizeBody); err != nil {
				return err
			}
		}

		y += pdfLineHeight
		if i == len(rows)-1 {
			y += pdfCellPadding
		}
	}

	c.pdf.SetXY(pdfMarginLeft, y+12)
	return nil
}

// drawNotes renders the Notes heading and wrapped body text, if any.
func (c *pdfCanvas) drawNotes(notes string) error {
	if err := c.setFont(pdfFontSizeHeading); err != nil {
		return err
	}

	headingHeight := pdfLineHeight + 4
	c.ensureSpace(headingHeight + pdfLineHeight)

	y := c.pdf.GetY()
	if err := c.cell(pdfMarginLeft, y, pdfContentWidth, "Notes"); err != nil {
		return err
	}
	y += headingHeight

	if err := c.setFont(pdfFontSizeBody); err != nil {
		return err
	}

	wrapped, err := c.pdf.SplitTextWithWordWrap(notes, pdfContentWidth)
	if err != nil {
		return fmt.Errorf("wrap notes: %w", err)
	}

	for _, line := range wrapped {
		if c.ensureSpace(pdfLineHeight) {
			y = c.pdf.GetY()
		}
		if err := c.cell(pdfMarginLeft, y, pdfContentWidth, line); err != nil {
			return err
		}
		y += pdfLineHeight
	}

	c.pdf.SetXY(pdfMarginLeft, y)
	return nil
}
