-- Terms & Conditions acceptance tracking (see registration_service.go
-- and admin.CurrentTermsVersion).
--
-- terms_accepted_at/terms_version record when, and to which version of
-- the Terms & Conditions, a user agreed at signup. Both are nullable:
-- every user created before this migration has no recorded acceptance,
-- and that is left as NULL rather than backfilled — there is no way to
-- know what, if anything, an existing user agreed to.
ALTER TABLE users
    ADD COLUMN terms_accepted_at TIMESTAMPTZ NULL,
    ADD COLUMN terms_version TEXT NULL;
