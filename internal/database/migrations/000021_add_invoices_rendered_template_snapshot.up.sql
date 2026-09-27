-- Custom invoice layouts (Phase 4): the template snapshot an issued
-- invoice was rendered with. Captured exactly once by
-- InvoiceService.Send at the Draft -> Sent transition — see
-- invoice.TemplateSnapshot for its JSON shape ({templateId, name,
-- definition, isSystem}) — and never rewritten afterward, the same
-- immutability guarantee migration 000014's party snapshot already
-- gives seller/customer details: editing or even deleting the template
-- an invoice used must never change how that invoice renders.
--
-- NULL for a Draft invoice (nothing captured yet — a Draft's PDF always
-- reflects the organisation's *current* default template) and for any
-- invoice sent before this feature existed, the same "nullable column,
-- honestly representing missing history" approach 000014 already used.
ALTER TABLE invoices
    ADD COLUMN rendered_template_snapshot JSONB NULL;
