--Each organisation can have multiple invoices, and each customer can have multiple invoices.
CREATE TABLE invoices (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    customer_id UUID NOT NULL
        REFERENCES customers(id),

    invoice_number TEXT NOT NULL,

    issue_date DATE NOT NULL,

    due_date DATE NOT NULL,

    total BIGINT NOT NULL DEFAULT 0,

    status TEXT NOT NULL DEFAULT 'draft',

    notes TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    CONSTRAINT invoices_organisation_number_unique
        UNIQUE (organisation_id, invoice_number)
);
