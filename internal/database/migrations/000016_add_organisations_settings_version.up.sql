--Milestone 13 Part 2: optimistic concurrency for PATCH /organisation and
--PATCH /organisation/settings. version is the resource's concurrency
--token: exposed only as the HTTP ETag, required back as If-Match, and
--incremented by exactly one on every user-facing update, whose UPDATE
--requires the expected version (WHERE ... AND version = $n) - so a stale
--write matches no row instead of silently overwriting a newer one.
--
--version counts user-facing modifications only, not every internal
--write: settings.invoice_number allocation (on every invoice creation)
--deliberately never increments it, since invoice_number isn't part of
--the settings resource clients edit. updated_at can't serve as the token
--for exactly that reason (and its JSON form has only second precision).
--
--Existing rows start at version 1.
ALTER TABLE organisations
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1;

ALTER TABLE settings
    ADD COLUMN version BIGINT NOT NULL DEFAULT 1;
