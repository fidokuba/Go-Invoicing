ALTER TABLE users
    DROP CONSTRAINT users_email_unique;

ALTER TABLE users
    ADD CONSTRAINT users_organisation_email_unique UNIQUE (organisation_id, email);
