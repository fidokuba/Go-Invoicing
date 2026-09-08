--Multiple address records can be associated with a single customer. Each address record belongs to one customer.
CREATE TABLE addresses (
    id UUID PRIMARY KEY,

    customer_id UUID NOT NULL
        REFERENCES customers(id),

    type TEXT NOT NULL,

    street TEXT,

    city TEXT,

    state TEXT,

    postal_code TEXT,

    country TEXT,

    is_default BOOLEAN NOT NULL DEFAULT FALSE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ
);