-- Cancelling invoices: invoices are never deleted, so a mistaken or
-- abandoned one is moved to status 'cancelled' instead (see
-- invoice.Invoice.Cancel). cancelled_at records when, set exactly once
-- at that transition and NULL for every invoice that isn't cancelled.
ALTER TABLE invoices
    ADD COLUMN cancelled_at TIMESTAMPTZ NULL;
