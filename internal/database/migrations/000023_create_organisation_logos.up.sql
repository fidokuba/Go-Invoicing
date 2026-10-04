-- Organisation logos, shown by the Logo block of a custom invoice layout.
--
-- Each upload is a new, immutable row; organisations.logo_id points at
-- the current one. Replacing or removing a logo never changes or deletes
-- an old row, because an issued invoice records the logo it was issued
-- with (invoices.seller_logo_id, captured alongside the rest of the
-- seller snapshot at Send) — the same "an issued invoice never changes
-- appearance" guarantee migration 000014 gives the seller's details.
-- Stored in PostgreSQL (BYTEA) rather than on disk, so a deployment
-- with no persistent filesystem loses nothing; uploads are capped at
-- 1 MiB by the API (admin.MaxOrganisationLogoBytes).
CREATE TABLE organisation_logos (
    id UUID PRIMARY KEY,

    organisation_id UUID NOT NULL
        REFERENCES organisations(id),

    content_type TEXT NOT NULL,

    data BYTEA NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX organisation_logos_organisation_id_idx
    ON organisation_logos (organisation_id);

-- The original logo TEXT column was never read or written by anything
-- (logo upload was always deferred); logo_id replaces it.
ALTER TABLE organisations
    DROP COLUMN logo,
    ADD COLUMN logo_id UUID NULL REFERENCES organisation_logos(id);

-- NULL for a Draft, for an invoice issued while the organisation had no
-- logo, and for any invoice issued before this feature existed.
ALTER TABLE invoices
    ADD COLUMN seller_logo_id UUID NULL REFERENCES organisation_logos(id);
