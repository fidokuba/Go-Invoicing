-- Custom invoice layouts (Phase 2): invoice_templates holds every
-- template an organisation has saved — plain user-created ones plus
-- exactly one permanent, non-deletable "Classic" row per organisation
-- (is_system = TRUE), seeded below for every existing organisation so
-- nothing breaks for an org that never customises anything.
--
-- definition is the template builder's own JSON (a Puck-shaped document
-- describing the arranged blocks) for a user template; for the seeded
-- Classic row it is a placeholder marker only — the renderer still
-- serves Classic via its own hardcoded HTML template (see
-- renderer/src/classicTemplate.js), not by interpreting this JSON. That
-- becomes real once Classic is expressed as an actual template
-- definition in a later phase.
--
-- is_default: at most one TRUE per organisation, enforced below by a
-- partial unique index. "At least one" is guaranteed differently: by
-- is_system's own invariant (a system template can never be deleted —
-- enforced in application code, since a partial unique index can't
-- express "this specific row must always exist") together with the
-- application rule that deleting the current default reverts the
-- organisation to its Classic template rather than leaving none set.
CREATE TABLE invoice_templates (
    id UUID PRIMARY KEY,
    organisation_id UUID NOT NULL REFERENCES organisations (id),
    name TEXT NOT NULL,
    definition JSONB NOT NULL,
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    version BIGINT NOT NULL DEFAULT 1
);

CREATE INDEX invoice_templates_organisation_id_idx
    ON invoice_templates (organisation_id)
    WHERE deleted_at IS NULL;

CREATE UNIQUE INDEX invoice_templates_one_default_per_org
    ON invoice_templates (organisation_id)
    WHERE is_default = TRUE AND deleted_at IS NULL;

-- Backfill: every organisation that exists today gets its permanent
-- Classic default. gen_random_uuid() is core PostgreSQL since v13 (this
-- project runs postgres:18 — see compose.yaml) — no extension needed.
INSERT INTO invoice_templates (id, organisation_id, name, definition, is_default, is_system)
SELECT gen_random_uuid(), id, 'Classic', '{"system":"classic"}'::jsonb, TRUE, TRUE
FROM organisations
WHERE deleted_at IS NULL;
