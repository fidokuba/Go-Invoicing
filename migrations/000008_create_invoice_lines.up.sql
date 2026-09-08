--Each invoice can have multiple lines, and each line stores its own historical values.
CREATE TABLE invoice_lines (
    id UUID PRIMARY KEY,

    invoice_id UUID NOT NULL
        REFERENCES invoices(id),

    product_id UUID
        REFERENCES products(id),

    description TEXT NOT NULL,

    quantity NUMERIC(12, 4) NOT NULL,

    unit_price BIGINT NOT NULL,

    vat_rate NUMERIC(5, 2) NOT NULL,

    vat_amount BIGINT NOT NULL,

    total BIGINT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
