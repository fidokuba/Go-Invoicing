--Milestone 4 Part 2: authentication looks a user up by email alone,
--before any organisation is known, so email uniqueness moves from
--per-organisation to global. The same email can no longer be reused by a
--second organisation.
ALTER TABLE users
    DROP CONSTRAINT users_organisation_email_unique;

ALTER TABLE users
    ADD CONSTRAINT users_email_unique UNIQUE (email);
