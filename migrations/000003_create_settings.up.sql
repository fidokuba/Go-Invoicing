--Each organisation has one setting record.
CREATE TABLE settings (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    invoice_prefix TEXT NOT NULL DEFAULT 'INV',

    invoice_number INTEGER NOT NULL DEFAULT 1,

    currency TEXT NOT NULL DEFAULT 'GBP',

    tax_rate NUMERIC(5, 2) NOT NULL DEFAULT 20.00,

    payment_terms INTEGER NOT NULL DEFAULT 30,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    CONSTRAINT settings_organisation_unique
        UNIQUE (organisation_id)
);