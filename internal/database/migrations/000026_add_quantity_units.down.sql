ALTER TABLE invoice_lines
    DROP COLUMN quantity_unit;

ALTER TABLE products
    DROP COLUMN quantity_unit;

ALTER TABLE settings
    DROP COLUMN quantity_units;
