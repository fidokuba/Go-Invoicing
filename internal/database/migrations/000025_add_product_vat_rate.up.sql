--A product's default VAT rate, as a percentage (20 means 20%), copied
--onto an invoice line's VAT % when the product is picked in the invoice
--editor (and still editable there). NULL means the product has no
--default, so picking it leaves the line's VAT % as it was. Same
--NUMERIC(5, 2) type as invoice_lines.vat_rate.
ALTER TABLE products
    ADD COLUMN vat_rate NUMERIC(5, 2)
        CONSTRAINT products_vat_rate_range CHECK (vat_rate >= 0 AND vat_rate <= 100);
