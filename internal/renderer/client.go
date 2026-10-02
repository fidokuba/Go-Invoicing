// Package renderer is the Go-side client for the invoice-template
// rendering service (Phase 1 proved the pipeline; Phase 4 wires it into
// InvoicePDFService as the path a real custom template renders through,
// alongside gopdf's continuing to serve the system Classic template —
// see internal/invoice/invoice_pdf_service.go's own doc comment on that
// split). It holds no PostgreSQL access and no HTTP handler of its own —
// a thin HTTP client, the wire-format DTOs it sends, and (Hardening
// pass) an optional shared-secret header, nothing else.
//
// This package deliberately does not import internal/invoice, even
// though RenderRequest mirrors InvoicePDFData field-for-field: the
// conversion (invoice.InvoicePDFData.ToRenderRequest) lives in that
// package instead, specifically so this one stays free to be imported
// by internal/invoice without a cycle.
package renderer

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// requestTimeout bounds one render call. Generous relative to the
// renderer service's own RENDER_TIMEOUT_MS (15s) so a slow render
// surfaces as the service's own timeout error, not this client's.
const requestTimeout = 20 * time.Second

// ErrRendererUnavailable is returned when the renderer service responds
// 503 (Hardening pass: renderer/src/server.js's own MAX_CONCURRENT_RENDERS
// guard) — a deliberate, self-imposed "at capacity, try again shortly"
// rejection, not a render actually failing. Kept as its own sentinel,
// distinct from every other non-2xx response, specifically so
// InvoicePDFService's caller can offer the client a meaningful 503
// instead of the generic 500 an unrecognised renderer failure gets (see
// invoice_handler.go's own GetPDF branch) — the same reasoning
// ErrInvoicePDFDataUnavailable already applies one layer up for
// repository lookups.
var ErrRendererUnavailable = errors.New("renderer service is at capacity")

// sharedSecretHeader is the one header name both this client and the
// renderer's own server.js agree on — see config.Config
// .RendererSharedSecret's own doc comment for why this exists at all.
const sharedSecretHeader = "X-Renderer-Shared-Secret"

// Client calls the renderer service's HTTP API. It holds no per-request
// state, so a single Client is safe to share and reuse across requests —
// the same shape as PostgresUserRepository and friends holding only a
// connection pool, never a request's own data.
type Client struct {
	baseURL      string
	sharedSecret string
	httpClient   *http.Client
}

// NewClient wires a client against the renderer service reachable at
// baseURL (e.g. "http://renderer:3000" inside Docker Compose, or
// "http://localhost:3000" for local development — see compose.yaml).
// sharedSecret is sent as a header on every request if non-empty (empty
// sends no header at all — see config.Config.RendererSharedSecret's own
// doc comment for when that's the correct setting, not an oversight).
func NewClient(baseURL string, sharedSecret string) *Client {
	return &Client{
		baseURL:      baseURL,
		sharedSecret: sharedSecret,
		httpClient:   &http.Client{Timeout: requestTimeout},
	}
}

// Seller mirrors invoice.InvoicePDFSeller's fields exactly, with JSON
// tags — kept as this package's own type (rather than adding JSON tags
// to invoice.InvoicePDFData itself) because that struct's own doc
// comment deliberately excludes "HTTP DTOs" from what it is; see
// FromInvoicePDFData for the one place the two are bridged.
type Seller struct {
	Name         string   `json:"name"`
	AddressLines []string `json:"addressLines"`
	Email        string   `json:"email"`
	Phone        string   `json:"phone"`
	Website      string   `json:"website"`
	TaxID        string   `json:"taxId"`
}

// Customer mirrors invoice.InvoicePDFCustomer.
type Customer struct {
	DisplayName  string   `json:"displayName"`
	AddressLines []string `json:"addressLines"`
	Email        string   `json:"email"`
	TaxID        string   `json:"taxId"`
}

// Line mirrors invoice.InvoicePDFLine.
type Line struct {
	Description string `json:"description"`
	Quantity    string `json:"quantity"`
	UnitPrice   string `json:"unitPrice"`
	VATRate     string `json:"vatRate"`
	VATAmount   string `json:"vatAmount"`
	Total       string `json:"total"`
}

// RenderRequest is the wire format POST /render accepts — see
// renderer/src/server.js for the exact same shape read there.
// Field-for-field (excluding Definition), this mirrors
// invoice.InvoicePDFData; that package's own
// InvoicePDFData.ToRenderRequest is the one conversion point between
// the two.
type RenderRequest struct {
	InvoiceNumber string `json:"invoiceNumber"`
	IssueDate     string `json:"issueDate"`
	DueDate       string `json:"dueDate"`
	StatusLabel   string `json:"statusLabel"`

	Seller   Seller   `json:"seller"`
	Customer Customer `json:"customer"`
	Lines    []Line   `json:"lines"`

	VATRegistered bool   `json:"vatRegistered"`
	Subtotal      string `json:"subtotal"`
	VATTotal      string `json:"vatTotal"`
	Total         string `json:"total"`

	ShowPaymentSummary bool   `json:"showPaymentSummary"`
	AmountPaid         string `json:"amountPaid"`
	AmountOutstanding  string `json:"amountOutstanding"`

	Notes string `json:"notes"`

	// Definition is the saved template's own document (a Puck-shaped
	// JSON blob — see template.Template.Definition) — nil/omitted for
	// the system Classic template, which the renderer service still
	// serves via its own hardcoded HTML (see renderer/src/classicTemplate.js),
	// not by interpreting this field. Only a real, user-created
	// template's RenderRequest ever sets it.
	Definition json.RawMessage `json:"definition,omitempty"`
}

// Render POSTs req to the renderer service and returns the resulting
// PDF bytes. A non-2xx response is surfaced as an error, not returned as
// if it were a valid PDF — callers never need to sniff the response
// themselves to find out it failed.
func (c *Client) Render(ctx context.Context, req RenderRequest) ([]byte, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal render request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/render", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build render request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.sharedSecret != "" {
		httpReq.Header.Set(sharedSecretHeader, c.sharedSecret)
	}

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("call renderer service: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read renderer response: %w", err)
	}

	if resp.StatusCode == http.StatusServiceUnavailable {
		return nil, fmt.Errorf("%w: %s", ErrRendererUnavailable, string(respBody))
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("renderer service returned %d: %s", resp.StatusCode, string(respBody))
	}

	return respBody, nil
}
