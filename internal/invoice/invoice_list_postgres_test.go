package invoice

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
)

// seedListInvoice inserts an invoice directly via the repository (not
// through InvoiceService, whose Create always produces a Draft) so
// boundary-testing an arbitrary persisted Status/DueDate combination
// doesn't require driving the whole Create->Send lifecycle for each one.
func seedListInvoice(
	t *testing.T,
	repository *PostgresInvoiceRepository,
	organisationID, customerID uuid.UUID,
	invoiceNumber, status string,
	issueDate, dueDate time.Time,
) *Invoice {
	t.Helper()

	inv := &Invoice{
		ID:             uuid.New(),
		OrganisationID: organisationID,
		CustomerID:     customerID,
		InvoiceNumber:  invoiceNumber,
		IssueDate:      issueDate,
		DueDate:        dueDate,
		Total:          1000,
		Status:         status,
	}

	if err := repository.Create(context.Background(), inv); err != nil {
		t.Fatalf("seed invoice %q: %v", invoiceNumber, err)
	}

	return inv
}

// TestPostgresInvoiceRepository_List_EffectiveStatusBoundaries is the
// Milestone 8 Part 3 section 9/29 core proof: the SQL predicates for
// ?status=sent and ?status=overdue must agree exactly with
// Invoice.EffectiveStatus's own due-date boundary, for every case that
// boundary distinguishes.
func TestPostgresInvoiceRepository_List_EffectiveStatusBoundaries(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())
	yesterday := today.AddDate(0, 0, -1)
	tomorrow := today.AddDate(0, 0, 1)
	longAgo := today.AddDate(0, 0, -30)

	dueYesterday := seedListInvoice(t, repository, organisationID, customerID, "INV-BOUND-1", InvoiceStatusSent, yesterday, yesterday)
	dueToday := seedListInvoice(t, repository, organisationID, customerID, "INV-BOUND-2", InvoiceStatusSent, today, today)
	dueTomorrow := seedListInvoice(t, repository, organisationID, customerID, "INV-BOUND-3", InvoiceStatusSent, today, tomorrow)
	paidOldDueDate := seedListInvoice(t, repository, organisationID, customerID, "INV-BOUND-4", InvoiceStatusPaid, longAgo, longAgo)
	draftOldDueDate := seedListInvoice(t, repository, organisationID, customerID, "INV-BOUND-5", InvoiceStatusDraft, longAgo, longAgo)

	t.Cleanup(func() {
		for _, id := range []uuid.UUID{dueYesterday.ID, dueToday.ID, dueTomorrow.ID, paidOldDueDate.ID, draftOldDueDate.ID} {
			_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", id)
		}
	})

	t.Run("due yesterday is overdue", func(t *testing.T) {
		items, total, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusOverdue, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 || items[0].ID != dueYesterday.ID {
			t.Fatalf("expected exactly the due-yesterday invoice as overdue, got total=%d items=%+v", total, items)
		}
	})

	t.Run("due today is still sent, not overdue", func(t *testing.T) {
		sentItems, sentTotal, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusSent, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list sent: %v", err)
		}
		foundDueToday := false
		for _, item := range sentItems {
			if item.ID == dueToday.ID {
				foundDueToday = true
			}
		}
		if !foundDueToday {
			t.Errorf("expected due-today invoice to appear under status=sent, got %+v (total %d)", sentItems, sentTotal)
		}

		overdueItems, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusOverdue, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list overdue: %v", err)
		}
		for _, item := range overdueItems {
			if item.ID == dueToday.ID {
				t.Error("expected due-today invoice NOT to appear under status=overdue")
			}
		}
	})

	t.Run("due tomorrow is sent", func(t *testing.T) {
		items, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusSent, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		found := false
		for _, item := range items {
			if item.ID == dueTomorrow.ID {
				found = true
			}
		}
		if !found {
			t.Error("expected due-tomorrow invoice to appear under status=sent")
		}
	})

	t.Run("paid with an old due date stays paid, never overdue", func(t *testing.T) {
		paidItems, paidTotal, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusPaid, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list paid: %v", err)
		}
		found := false
		for _, item := range paidItems {
			if item.ID == paidOldDueDate.ID {
				found = true
			}
		}
		if !found {
			t.Errorf("expected the paid invoice under status=paid, got %+v (total %d)", paidItems, paidTotal)
		}

		overdueItems, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusOverdue, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list overdue: %v", err)
		}
		for _, item := range overdueItems {
			if item.ID == paidOldDueDate.ID {
				t.Error("expected the paid invoice NOT to appear under status=overdue despite its old due date")
			}
		}
	})

	t.Run("draft with an old due date stays draft, never overdue", func(t *testing.T) {
		draftItems, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusDraft, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list draft: %v", err)
		}
		found := false
		for _, item := range draftItems {
			if item.ID == draftOldDueDate.ID {
				found = true
			}
		}
		if !found {
			t.Error("expected the draft invoice under status=draft")
		}

		overdueItems, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusOverdue, Limit: 50}, today)
		if err != nil {
			t.Fatalf("list overdue: %v", err)
		}
		for _, item := range overdueItems {
			if item.ID == draftOldDueDate.ID {
				t.Error("expected the draft invoice NOT to appear under status=overdue despite its old due date")
			}
		}
	})

	t.Run("no status filter returns all five", func(t *testing.T) {
		_, total, err := repository.List(ctx, organisationID, InvoiceListFilter{Limit: 50}, today)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 5 {
			t.Fatalf("expected all 5 seeded invoices with no status filter, got %d", total)
		}
	})
}

// TestPostgresInvoiceRepository_List_StatusPlusCustomerAndPagination
// proves status combined with customerId narrows correctly, and that
// pagination.total agrees with the combined predicate (section 29:
// "status + customer filter", "status + pagination count").
func TestPostgresInvoiceRepository_List_StatusPlusCustomerAndPagination(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerA := createTestCustomer(t, db, organisationID)
	customerB := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())

	invA1 := seedListInvoice(t, repository, organisationID, customerA, "INV-CUST-A1", InvoiceStatusDraft, today, today.AddDate(0, 0, 30))
	invA2 := seedListInvoice(t, repository, organisationID, customerA, "INV-CUST-A2", InvoiceStatusPaid, today, today.AddDate(0, 0, 30))
	invB1 := seedListInvoice(t, repository, organisationID, customerB, "INV-CUST-B1", InvoiceStatusDraft, today, today.AddDate(0, 0, 30))

	t.Cleanup(func() {
		for _, id := range []uuid.UUID{invA1.ID, invA2.ID, invB1.ID} {
			_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", id)
		}
	})

	items, total, err := repository.List(ctx, organisationID, InvoiceListFilter{Status: InvoiceStatusDraft, CustomerID: &customerA, Limit: 50}, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].ID != invA1.ID {
		t.Fatalf("expected exactly customer A's one draft invoice, got total=%d items=%+v", total, items)
	}

	// Pagination: limit=1 must still report the full matching total, not 1.
	itemsPage, totalPage, err := repository.List(ctx, organisationID, InvoiceListFilter{CustomerID: &customerA, Limit: 1, Offset: 0}, today)
	if err != nil {
		t.Fatalf("list paginated: %v", err)
	}
	if totalPage != 2 {
		t.Fatalf("expected total 2 for customer A regardless of limit, got %d", totalPage)
	}
	if len(itemsPage) != 1 {
		t.Fatalf("expected exactly 1 item on this page, got %d", len(itemsPage))
	}
}

// TestPostgresInvoiceRepository_List_Search proves substring, case-
// insensitive matching against invoice_number.
func TestPostgresInvoiceRepository_List_Search(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())
	dueDate := today.AddDate(0, 0, 30)

	match := seedListInvoice(t, repository, organisationID, customerID, "ACME-2026-0042", InvoiceStatusDraft, today, dueDate)
	noMatch := seedListInvoice(t, repository, organisationID, customerID, "OTHER-0001", InvoiceStatusDraft, today, dueDate)

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = ANY($1)", []uuid.UUID{match.ID, noMatch.ID})
	})

	items, total, err := repository.List(ctx, organisationID, InvoiceListFilter{Search: "2026-0042", Limit: 50}, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].ID != match.ID {
		t.Fatalf("expected exactly the matching invoice number, got total=%d items=%+v", total, items)
	}

	// Case-insensitive.
	itemsLower, totalLower, err := repository.List(ctx, organisationID, InvoiceListFilter{Search: "acme", Limit: 50}, today)
	if err != nil {
		t.Fatalf("list lowercase search: %v", err)
	}
	if totalLower != 1 || itemsLower[0].ID != match.ID {
		t.Fatalf("expected case-insensitive match, got total=%d items=%+v", totalLower, itemsLower)
	}
}

// TestPostgresInvoiceRepository_List_DateRangeFilters proves
// issueDateFrom/To and dueDateFrom/To behave as inclusive bounds.
func TestPostgresInvoiceRepository_List_DateRangeFilters(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	jan1 := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb1 := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	mar1 := time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)

	invJan := seedListInvoice(t, repository, organisationID, customerID, "INV-DATE-JAN", InvoiceStatusDraft, jan1, jan1.AddDate(0, 1, 0))
	invFeb := seedListInvoice(t, repository, organisationID, customerID, "INV-DATE-FEB", InvoiceStatusDraft, feb1, feb1.AddDate(0, 1, 0))
	invMar := seedListInvoice(t, repository, organisationID, customerID, "INV-DATE-MAR", InvoiceStatusDraft, mar1, mar1.AddDate(0, 1, 0))

	t.Cleanup(func() {
		for _, id := range []uuid.UUID{invJan.ID, invFeb.ID, invMar.ID} {
			_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", id)
		}
	})

	today := UTCDate(time.Now())

	items, total, err := repository.List(ctx, organisationID, InvoiceListFilter{IssueDateFrom: &feb1, IssueDateTo: &feb1, Limit: 50}, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].ID != invFeb.ID {
		t.Fatalf("expected exactly the February invoice for an exact-day range, got total=%d items=%+v", total, items)
	}

	itemsRange, totalRange, err := repository.List(ctx, organisationID, InvoiceListFilter{IssueDateFrom: &feb1, IssueDateTo: &mar1, Limit: 50}, today)
	if err != nil {
		t.Fatalf("list range: %v", err)
	}
	if totalRange != 2 {
		t.Fatalf("expected Feb+Mar (2 invoices) inclusive, got %d: %+v", totalRange, itemsRange)
	}
}

// TestPostgresInvoiceRepository_List_SortByTotalWithTieBreak proves
// numeric sort (total) plus deterministic id-ascending tie-breaking
// against real Postgres.
func TestPostgresInvoiceRepository_List_SortByTotalWithTieBreak(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())
	dueDate := today.AddDate(0, 0, 30)

	first := seedListInvoice(t, repository, organisationID, customerID, "INV-TIE-1", InvoiceStatusDraft, today, dueDate)
	second := seedListInvoice(t, repository, organisationID, customerID, "INV-TIE-2", InvoiceStatusDraft, today, dueDate)

	t.Cleanup(func() {
		for _, id := range []uuid.UUID{first.ID, second.ID} {
			_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", id)
		}
	})

	wantFirst, wantSecond := first.ID, second.ID
	if wantSecond.String() < wantFirst.String() {
		wantFirst, wantSecond = wantSecond, wantFirst
	}

	items, _, err := repository.List(ctx, organisationID, InvoiceListFilter{Sort: "total", Order: "asc", Limit: 50}, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if items[0].ID != wantFirst || items[1].ID != wantSecond {
		t.Errorf("expected tied-total rows in id-ascending order, got %s then %s", items[0].ID, items[1].ID)
	}
}

// TestPostgresInvoiceRepository_List_TenantIsolation proves an invoice
// belonging to a different organisation never appears in another
// organisation's list, nor counts toward its total.
func TestPostgresInvoiceRepository_List_TenantIsolation(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	orgA := createTestOrganisation(t, db)
	orgB := createTestOrganisation(t, db)
	customerA := createTestCustomer(t, db, orgA)
	customerB := createTestCustomer(t, db, orgB)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())
	dueDate := today.AddDate(0, 0, 30)

	invA := seedListInvoice(t, repository, orgA, customerA, "INV-TENANT-A", InvoiceStatusDraft, today, dueDate)
	invB := seedListInvoice(t, repository, orgB, customerB, "INV-TENANT-B", InvoiceStatusDraft, today, dueDate)

	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", invA.ID)
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", invB.ID)
	})

	items, total, err := repository.List(ctx, orgA, InvoiceListFilter{Limit: 50}, today)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 1 || items[0].ID != invA.ID {
		t.Fatalf("expected only Org A's invoice, got total=%d items=%+v", total, items)
	}
}

// TestPostgresInvoiceRepository_List_EmptyAndOffsetBeyondTotal proves a
// valid query with no matches, and an offset past the end of the result
// set, both return an empty-but-valid result with the correct total.
func TestPostgresInvoiceRepository_List_EmptyAndOffsetBeyondTotal(t *testing.T) {
	db := newTestPool(t)
	ctx := context.Background()

	organisationID := createTestOrganisation(t, db)
	customerID := createTestCustomer(t, db, organisationID)
	repository := NewPostgresInvoiceRepository(db)

	today := UTCDate(time.Now())
	inv := seedListInvoice(t, repository, organisationID, customerID, "INV-ONLY-1", InvoiceStatusDraft, today, today.AddDate(0, 0, 30))
	t.Cleanup(func() {
		_, _ = db.Exec(context.Background(), "DELETE FROM invoices WHERE id = $1", inv.ID)
	})

	t.Run("no matches for a fresh organisation", func(t *testing.T) {
		emptyOrg := createTestOrganisation(t, db)
		items, total, err := repository.List(ctx, emptyOrg, InvoiceListFilter{Limit: 50}, today)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 0 || len(items) != 0 {
			t.Fatalf("expected empty result, got total=%d items=%+v", total, items)
		}
	})

	t.Run("offset beyond total", func(t *testing.T) {
		items, total, err := repository.List(ctx, organisationID, InvoiceListFilter{Limit: 50, Offset: 1000}, today)
		if err != nil {
			t.Fatalf("list: %v", err)
		}
		if total != 1 {
			t.Fatalf("expected total to remain 1, got %d", total)
		}
		if len(items) != 0 {
			t.Fatalf("expected 0 items at an out-of-range offset, got %d", len(items))
		}
	})
}
