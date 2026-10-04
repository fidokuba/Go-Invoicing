ALTER TABLE invoices
    DROP COLUMN seller_logo_id;

ALTER TABLE organisations
    DROP COLUMN logo_id,
    ADD COLUMN logo TEXT;

DROP TABLE organisation_logos;
