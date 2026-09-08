--Every user must belong to an organisation, and each organisation can have multiple users.
--The combination of organisation_id and email must be unique to ensure that no two users within the same organisation can have the same email address.
--This is important for user identification and authentication within the context of an organisation.
CREATE TABLE users (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    name TEXT NOT NULL,

    email TEXT NOT NULL,

    password_hash TEXT NOT NULL,

    role TEXT NOT NULL,

    is_active BOOLEAN NOT NULL DEFAULT TRUE,

    last_login TIMESTAMPTZ,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    deleted_at TIMESTAMPTZ,

    CONSTRAINT users_organisation_email_unique
        UNIQUE (organisation_id, email)
);