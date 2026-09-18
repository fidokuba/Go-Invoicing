--Milestone 5: sent_at records the moment an invoice was finalised
--(Draft -> Sent), set exactly once by InvoiceService.Send alongside the
--status change, in the same UPDATE statement — never independently, so
--the two can never observably disagree. NULL for every invoice that has
--never been sent (including invoices created before this migration).
ALTER TABLE invoices
    ADD COLUMN sent_at TIMESTAMPTZ;
