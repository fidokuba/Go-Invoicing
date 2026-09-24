--Milestone 13 Part 3: indexes justified by measured EXPLAIN (ANALYZE)
--plans on a multi-tenant dataset (~48k invoices, 48k payments, 144k
--invoice lines, 40k customers). PostgreSQL does not index foreign-key
--columns automatically, and none of these lookups had a usable index,
--so each one scanned the whole table - every tenant's rows - to find a
--handful. Only these four were added; see the M13.3 report for the
--queries deliberately left alone.

--Per-invoice payment reads: GetTotalPaidByInvoiceID (payment creation,
--invoice detail), GetTotalPaidByInvoiceIDs (every invoice list page)
--and GetByInvoiceID. payments_invoice_idempotency_key_unique can't serve
--these: it's partial (keyed rows only), so it can't see historical
--payments that must still be summed.
CREATE INDEX payments_invoice_id_idx ON payments (invoice_id);

--GetLinesByInvoiceID: invoice detail, Send and PDF generation.
CREATE INDEX invoice_lines_invoice_id_idx ON invoice_lines (invoice_id);

--GET /invoices' default order (issue_date DESC, id ASC) within a tenant:
--lets the first page be read in index order instead of scanning and
--top-N sorting every invoice. Other sort orders are not indexed.
CREATE INDEX invoices_organisation_issue_date_idx ON invoices (organisation_id, issue_date DESC, id);

--customers had no organisation_id index at all, so every customer list
--or search scanned all tenants' customers. Ordered to match GET
--/customers' default sort (name ASC, id ASC).
CREATE INDEX customers_organisation_name_idx ON customers (organisation_id, name, id);
