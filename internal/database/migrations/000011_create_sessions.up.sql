--Sessions are PostgreSQL-backed, server-side records of a successful
--login (Milestone 4 Part 2). The raw bearer token handed to the client is
--never stored: token_hash is its SHA-256 hash, the same principle as
--never storing a plaintext password.
--A session belongs to a user, not directly to an organisation -
--organisation identity is resolved via users.organisation_id, so there is
--deliberately no organisation_id column here.
--revoked_at exists so a future logout/session-revocation feature has
--somewhere to record that without a further schema change; nothing sets
--it yet.
CREATE TABLE sessions (
    id UUID PRIMARY KEY,

    user_id UUID NOT NULL
        REFERENCES users(id),

    token_hash TEXT NOT NULL,

    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    expires_at TIMESTAMPTZ NOT NULL,

    revoked_at TIMESTAMPTZ
);

CREATE UNIQUE INDEX sessions_token_hash_unique ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
