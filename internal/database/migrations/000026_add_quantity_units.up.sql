--Quantity units ("Day(s)", "KG", ...). An organisation keeps its own
--list of units in its settings; a product can carry a default unit from
--that list, copied onto an invoice line when the product is picked in
--the invoice editor (and still changeable there). The line keeps its own
--copy, as text: a unit later renamed or removed from the list never
--changes an existing product or invoice.
--
--Every organisation, existing and new, starts with the same five units.
ALTER TABLE settings
    ADD COLUMN quantity_units TEXT[] NOT NULL
        DEFAULT ARRAY['Day(s)', 'Hour(s)', 'KG', 'g', 'Pcs']::TEXT[];

ALTER TABLE products
    ADD COLUMN quantity_unit TEXT;

ALTER TABLE invoice_lines
    ADD COLUMN quantity_unit TEXT;
