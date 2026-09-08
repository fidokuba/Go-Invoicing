--Each invoice can have multiple payments, so invoice_id is not unique.
CREATE TABLE payments (
    id UUID PRIMARY KEY,

    invoice_id UUID NOT NULL
        REFERENCES invoices(id),

    amount BIGINT NOT NULL,

    payment_method TEXT NOT NULL,

    payment_date DATE NOT NULL,

    reference TEXT,

    notes TEXT,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
