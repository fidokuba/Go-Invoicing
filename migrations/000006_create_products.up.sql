--Each organisation can have multiple products. Each product belongs to one organisation.
CREATE TABLE products (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    name TEXT NOT NULL,

    description TEXT,

    sku TEXT NOT NULL,

    price BIGINT NOT NULL,

    category TEXT,

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    CONSTRAINT products_organisation_sku_unique
        UNIQUE (organisation_id, sku)
);