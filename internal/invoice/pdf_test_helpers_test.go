package invoice

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ledongthuc/pdf"
)

// extractPDFText and countPDFPages use github.com/ledongthuc/pdf, a
// pure-Go, test-only dependency added specifically to verify generated
// PDF content/structure without OCR, Poppler, pdftotext, Ghostscript or
// any other external binary (Milestone 7 Part 3 section 35). It's never
// imported by production code — only by tests in this package — so it
// adds no weight to the actual application binary.
//
// Text extracted from a positioned-text PDF like the ones this renderer
// produces can merge two adjacent, unrelated cell values with no space
// between them when their glyphs sit close together (e.g. a row's
// wrapped last description line running into the next column's value) —
// an inherent quirk of reconstructing plain text from positioned glyph
// runs, not a rendering defect. Tests here assert that expected
// substrings are present, not exact spacing, for exactly that reason.

func extractPDFText(t *testing.T, pdfBytes []byte) string {
	t.Helper()

	reader, err := pdf.NewReader(bytes.NewReader(pdfBytes), int64(len(pdfBytes)))
	if err != nil {
		t.Fatalf("open pdf for text extraction: %v", err)
	}

	textReader, err := reader.GetPlainText()
	if err != nil {
		t.Fatalf("get pdf plain text: %v", err)
	}

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(textReader); err != nil {
		t.Fatalf("read pdf plain text: %v", err)
	}

	return buf.String()
}

func countPDFPages(t *testing.T, pdfBytes []byte) int {
	t.Helper()

	reader, err := pdf.NewReader(bytes.NewReader(pdfBytes), int64(len(pdfBytes)))
	if err != nil {
		t.Fatalf("open pdf for page count: %v", err)
	}

	return reader.NumPage()
}

func requireValidPDFHeader(t *testing.T, pdfBytes []byte) {
	t.Helper()

	if len(pdfBytes) == 0 {
		t.Fatal("expected non-empty PDF bytes")
	}

	if !strings.HasPrefix(string(pdfBytes), "%PDF-") {
		t.Fatalf("expected PDF to start with %%PDF-, got %q", string(pdfBytes[:min(20, len(pdfBytes))]))
	}
}
