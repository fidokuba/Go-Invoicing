--Each organisation can have multiple customers. Each customer belongs to one organisation.
CREATE TABLE customers (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    name TEXT NOT NULL,

    email TEXT,

    phone TEXT,

    company_name TEXT,

    tax_id TEXT,

    status TEXT NOT NULL DEFAULT 'active',

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);