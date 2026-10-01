// Package sampledata provides a single, admin-only action — "Create
// Test Data" — that seeds an organisation with realistic-looking fake
// customers, products, and invoices in varying shapes (Draft, Sent, and
// Paid; single-line and multi-line) so a real environment has something
// to click around and test against, without hand-entering it.
//
// (Named "sampledata", not "testdata": the latter is a directory name
// Go's own tooling treats specially — ignored by go build/go test and
// dependency resolution — so a package genuinely called that is
// unbuildable, not just confusingly named.)
//
// This is pure orchestration, not a new domain: every record it creates
// goes through the exact same CustomerService/ProductService/
// InvoiceService validation and persistence path a real API client
// would use (the same reasoning RegistrationService's
// TemplateProvisioner hook already applies one layer down — reuse the
// real service, never a repository directly, so "fake" data is
// indistinguishable from real data once created). There is no new table
// and no new repository.
package sampledata

import (
	"context"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	"github.com/google/uuid"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
)

// Service generates a batch of fake customers, products, and invoices
// for one organisation.
type Service struct {
	organisationRepository admin.OrganisationRepository
	customerService        *customer.CustomerService
	productService         *product.ProductService
	invoiceService         *invoice.InvoiceService
}

func NewService(
	organisationRepository admin.OrganisationRepository,
	customerService *customer.CustomerService,
	productService *product.ProductService,
	invoiceService *invoice.InvoiceService,
) *Service {
	return &Service{
		organisationRepository: organisationRepository,
		customerService:        customerService,
		productService:         productService,
		invoiceService:         invoiceService,
	}
}

// Summary is what one call to Generate actually created — returned so
// the caller (and the Settings page) can show something concrete rather
// than a bare "done".
type Summary struct {
	CustomersCreated int
	ProductsCreated  int
	InvoicesCreated  int
	InvoicesSent     int
	InvoicesPaid     int
}

// Generate creates 5 fake customers, a shared pool of 6-12 fake
// products (varying each run), and for each customer a varying number
// of invoices (1-4) against a varying selection of those products —
// deliberately including both single-line and multi-line invoices, and
// a mix of Draft, Sent, and Paid outcomes (some Sent invoices are also
// backdated so they show as Overdue, per EffectiveStatus's own derived
// rule — exercising that state too without a separate code path for
// it).
//
// Every record is created through the organisation's real
// Customer/Product/InvoiceService — the same validation, the same
// invoice-numbering sequence, the same VAT-registration rule (an
// unregistered organisation gets 0% lines; a registered one gets a
// realistic 20%) a genuine API client would go through. Nothing here
// is simulated or backdoored.
//
// Safe to call repeatedly: every generated name/SKU/email includes a
// per-run random suffix, so re-running this never collides with a
// previous run's data (SKU is the one genuinely unique constraint in
// play — see migration 000006).
func (s *Service) Generate(ctx context.Context, organisationID uuid.UUID) (Summary, error) {
	organisation, err := s.organisationRepository.GetByID(ctx, organisationID)
	if err != nil {
		return Summary{}, fmt.Errorf("look up organisation for test data: %w", err)
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	runSuffix := strconv.FormatInt(time.Now().UnixNano()%1_000_000, 10)

	products, err := s.createProducts(ctx, organisationID, runSuffix, rng)
	if err != nil {
		return Summary{}, fmt.Errorf("create test products: %w", err)
	}

	customers, err := s.createCustomers(ctx, organisationID, runSuffix)
	if err != nil {
		return Summary{}, fmt.Errorf("create test customers: %w", err)
	}

	summary := Summary{
		CustomersCreated: len(customers),
		ProductsCreated:  len(products),
	}

	// Guarantees required by the feature request, not left to chance:
	// across the whole batch, at least one invoice has exactly one line
	// and at least one has several. forcedLineCounts is consumed first,
	// then every remaining invoice gets a random 1-4 lines.
	forcedLineCounts := []int{1, 4}

	for _, cust := range customers {
		numInvoices := 1 + rng.Intn(4) // 1..4
		for j := 0; j < numInvoices; j++ {
			lineCount := 1 + rng.Intn(4) // 1..4
			if len(forcedLineCounts) > 0 {
				lineCount = forcedLineCounts[0]
				forcedLineCounts = forcedLineCounts[1:]
			}

			inv, err := s.createInvoice(ctx, organisationID, cust.ID, products, organisation.VATRegistered, lineCount, rng)
			if err != nil {
				return summary, fmt.Errorf("create test invoice for customer %s: %w", cust.ID, err)
			}
			summary.InvoicesCreated++

			switch rng.Intn(3) {
			case 0:
				// Stays Draft.
			case 1, 2:
				sent, err := s.invoiceService.Send(ctx, organisationID, inv.ID)
				if err != nil {
					return summary, fmt.Errorf("send test invoice %s: %w", inv.ID, err)
				}
				summary.InvoicesSent++

				if rng.Intn(3) == 0 {
					if _, err := s.invoiceService.CreatePayment(ctx, organisationID, inv.ID, invoice.CreatePaymentRequest{
						Amount:         sent.Total,
						PaymentMethod:  "bank_transfer",
						IdempotencyKey: uuid.New().String(),
					}); err != nil {
						return summary, fmt.Errorf("record payment for test invoice %s: %w", inv.ID, err)
					}
					summary.InvoicesPaid++
				}
			}
		}
	}

	return summary, nil
}
