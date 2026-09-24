--VAT registration. A UK business that isn't VAT registered (e.g. a sole
--trader under the threshold) must not charge or show VAT on its invoices.
--
--organisations.vat_registered is the organisation's current status. It
--defaults to FALSE for every organisation, existing and new: VAT is only
--shown once an administrator explicitly says the business is registered.
--
--invoices.vat_registered is the status the invoice was created under,
--captured once by InvoiceService.Create and never rewritten, so an
--invoice keeps rendering the way it was issued even if the organisation's
--status later changes. Existing invoices that carry any VAT were issued
--as VAT invoices, so they are backfilled TRUE; the rest stay FALSE.
ALTER TABLE organisations
    ADD COLUMN vat_registered BOOLEAN NOT NULL DEFAULT FALSE;

ALTER TABLE invoices
    ADD COLUMN vat_registered BOOLEAN NOT NULL DEFAULT FALSE;

UPDATE invoices
SET vat_registered = TRUE
WHERE vat_total <> 0
    OR EXISTS (
        SELECT 1
        FROM invoice_lines
        WHERE invoice_lines.invoice_id = invoices.id
            AND invoice_lines.vat_rate <> 0
    );
