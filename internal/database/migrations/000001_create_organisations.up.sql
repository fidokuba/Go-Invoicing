--UUIDs because this application is designed as a multi-organisation application and UUIDs avoid exposing sequential database IDs.
--TIMESTAMPTZ stores an absolute point in time and works naturally with Go's time.Time.

CREATE TABLE organisations (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT,
    phone TEXT,
    website TEXT,
    logo TEXT,
    address TEXT,
    city TEXT,
    state TEXT,
    postal_code TEXT,
    country TEXT,
    tax_id TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);