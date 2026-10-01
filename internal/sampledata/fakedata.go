package sampledata

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/google/uuid"

	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"
)

const dateLayout = "2006-01-02"

// fakeCustomerNames is a fixed pool of plausible UK-style business
// names — Generate always takes the first 5, so repeated runs produce
// the same 5 names (only their email/suffix varies), which keeps the
// generated data recognisable as "the test batch" rather than an
// ever-growing pile of random strings.
var fakeCustomerNames = []string{
	"Acme Trading Ltd",
	"Blue Horizon Consulting",
	"Northfield Builders",
	"Riverside Bakery",
	"Evergreen Landscaping",
}

var fakeCities = []string{"London", "Manchester", "Bristol", "Leeds", "Edinburgh"}

// fakeProductCatalogue is a pool of plausible product/service names,
// each with a category and a price band (in minor units, i.e. pence) —
// createProducts draws a random 6-12 of these per run, so the catalogue
// size genuinely varies run to run, as asked for.
var fakeProductCatalogue = []struct {
	name        string
	category    string
	description string
	minPrice    int64
	maxPrice    int64
}{
	{"Consulting (half day)", "Services", "Half a day of on-site consulting", 20000, 35000},
	{"Consulting (full day)", "Services", "A full day of on-site consulting", 40000, 65000},
	{"Website Design Package", "Design", "A complete small-business website design", 150000, 400000},
	{"Logo Design", "Design", "A custom logo with two revision rounds", 30000, 80000},
	{"Monthly Support Retainer", "Support", "Ongoing monthly technical support", 50000, 120000},
	{"Software License (annual)", "Software", "Annual license for the core platform", 60000, 200000},
	{"Training Workshop (half day)", "Training", "An on-site team training session", 25000, 45000},
	{"Hardware Installation", "Hardware", "On-site installation and setup", 15000, 30000},
	{"Project Management (weekly)", "Services", "Dedicated project management, billed weekly", 80000, 150000},
	{"Cloud Hosting (monthly)", "Hosting", "Managed cloud hosting", 5000, 25000},
	{"Security Audit", "Services", "A full security review and report", 100000, 250000},
	{"Content Writing (per article)", "Marketing", "One SEO-optimised article", 8000, 20000},
}

// createProducts creates a random 6-12 products drawn from
// fakeProductCatalogue, each with a per-run-unique SKU (products_
// organisation_sku_unique — migration 000006 — is the one real
// uniqueness constraint a repeated run could otherwise collide with).
func (s *Service) createProducts(
	ctx context.Context,
	organisationID uuid.UUID,
	runSuffix string,
	rng *rand.Rand,
) ([]*product.Product, error) {
	count := 6 + rng.Intn(7) // 6..12
	indices := rng.Perm(len(fakeProductCatalogue))[:count]

	products := make([]*product.Product, 0, count)
	for i, idx := range indices {
		entry := fakeProductCatalogue[idx]
		price := entry.minPrice + rng.Int63n(entry.maxPrice-entry.minPrice+1)

		p, err := s.productService.Create(
			ctx,
			organisationID,
			entry.name,
			entry.description,
			fmt.Sprintf("TEST-%s-%02d", runSuffix, i+1),
			price,
			entry.category,
		)
		if err != nil {
			return nil, err
		}
		products = append(products, p)
	}

	return products, nil
}

// createCustomers creates the 5 fixed fakeCustomerNames, each with a
// made-up email/phone/company and a plausible UK billing address. A
// customer's name is intentionally stable across runs (see
// fakeCustomerNames); only the email's suffix (and hence the
// customer's identity) varies, so two runs produce 10 distinct
// customers rather than 5 duplicated ones.
func (s *Service) createCustomers(
	ctx context.Context,
	organisationID uuid.UUID,
	runSuffix string,
) ([]*customer.Customer, error) {
	customers := make([]*customer.Customer, 0, len(fakeCustomerNames))

	for i, name := range fakeCustomerNames {
		email := fmt.Sprintf("contact+test-%s-%d@example.com", runSuffix, i+1)
		phone := fmt.Sprintf("+44 20 7946 %04d", 1000+i)

		c, err := s.customerService.Create(ctx, organisationID, name, email, phone, name, "")
		if err != nil {
			return nil, err
		}

		city := fakeCities[i%len(fakeCities)]
		if _, err := s.customerService.UpsertBillingAddress(ctx, organisationID, c.ID, customer.UpsertBillingAddressRequest{
			Street:     fmt.Sprintf("%d High Street", 10+i),
			City:       city,
			PostalCode: "SW1A 1AA",
			Country:    "GB",
		}); err != nil {
			return nil, fmt.Errorf("set billing address for %s: %w", name, err)
		}

		customers = append(customers, c)
	}

	return customers, nil
}

// createInvoice builds one invoice for customerID with lineCount lines,
// each line a random product from products at a random quantity (and,
// for roughly a third of invoices, backdated into the past so a Sent,
// unpaid invoice naturally shows as Overdue via Invoice.EffectiveStatus
// — a real derived state, not a separate code path faking it).
func (s *Service) createInvoice(
	ctx context.Context,
	organisationID uuid.UUID,
	customerID uuid.UUID,
	products []*product.Product,
	vatRegistered bool,
	lineCount int,
	rng *rand.Rand,
) (*invoice.Invoice, error) {
	now := time.Now().UTC()
	issueDate := now
	dueDate := now.AddDate(0, 0, 14+rng.Intn(17)) // due 14-30 days after issue

	if rng.Intn(3) == 0 {
		// Backdate so this invoice's due date has already passed —
		// combined with Send below, this is what makes some generated
		// invoices show as Overdue.
		issueDate = now.AddDate(0, 0, -(30 + rng.Intn(31))) // issued 30-60 days ago
		dueDate = issueDate.AddDate(0, 0, 14)
	}

	lines := make([]invoice.CreateInvoiceLineRequest, lineCount)
	for i := 0; i < lineCount; i++ {
		p := products[rng.Intn(len(products))]
		productID := p.ID.String()
		quantity := float64(1 + rng.Intn(5))
		vatRate := 0.0
		if vatRegistered {
			vatRate = 20
		}

		lines[i] = invoice.CreateInvoiceLineRequest{
			ProductID:   &productID,
			Description: p.Name,
			Quantity:    quantity,
			UnitPrice:   p.Price,
			VATRate:     vatRate,
		}
	}

	inv, _, _, err := s.invoiceService.Create(ctx, organisationID, invoice.CreateInvoiceRequest{
		CustomerID: customerID.String(),
		IssueDate:  issueDate.Format(dateLayout),
		DueDate:    dueDate.Format(dateLayout),
		Lines:      lines,
		Notes:      "Generated by Create Test Data.",
	})
	if err != nil {
		return nil, err
	}

	return inv, nil
}
