# Release recovery and failure modes

This document is deliberately narrow: what to do when something about
cutting a release goes wrong. For the normal, happy-path release procedure,
see the README's "Releases" section — start there. Come here only once
something has already failed or looks wrong.

Nothing in this document requires (or suggests) creating a new release,
tag, or GitHub Actions run to test — every scenario below is either
reasoned from `.github/workflows/release.yml`'s actual logic and
`cmd/release`'s own validation, or was observed during the real
`v0.1.0-rc.1` release.

## The workflow's own safety net

`.github/workflows/release.yml` runs three jobs:
`validate` → (`native` + `container`, in parallel) → `publish`. The two
publish actions do **not** both live in the `publish` job — this is
important for reasoning about what can and can't go wrong:

- **`validate`** rejects a malformed tag (via `cmd/release -validate-only`)
  before anything is built. No later job runs if this fails.
- **`native`** builds and uploads the five cross-compiled binaries plus
  `checksums.txt` as a staging artifact (not yet public). It does not
  publish anything externally.
- **`container`** builds the multi-arch image, smoke-tests it, **and
  pushes it to GHCR itself** (`docker/build-push-action` with `push: true`,
  this job's own `packages: write` permission) — the GHCR publish happens
  here, not in `publish`. By the time this job succeeds, the container
  image is already live on GHCR.
- **`publish`** (`needs: [validate, native, container]`, `contents: write`
  only — no `packages: write`) runs only after `native` and `container`
  have both already succeeded, downloads `native`'s staged artifacts, and
  creates the GitHub Release. It never touches GHCR.

This means a failure in `validate` or `native` is always safe to walk away
from: nothing has been published, so there is nothing to clean up. A
failure in `container` before its own push step is likewise safe. The one
stage that needs a real decision is a **`publish` failure that happens
after `container` has already succeeded** — see below; because `publish`
can only ever run once `container` has already pushed to GHCR, the
reverse ordering (a GitHub Release existing with no corresponding GHCR
image) cannot occur in this architecture.

## Recovery procedures, by failure point

**`validate` fails (bad tag format):**
Delete the local and remote tag, fix it, and re-tag:

```sh
git tag -d v1.2.3
git push origin :refs/tags/v1.2.3
git tag v1.2.3-corrected   # or the corrected version
git push origin v1.2.3-corrected
```

Nothing was published — this is a clean do-over, not a recovery.

**`native` fails, or `container` fails before its push step (a real build
error):**
Nothing was published. Fix the underlying problem, delete the tag, and
re-tag exactly as above. Re-pushing the *same* tag name after deleting it
remotely is fine here specifically because `publish` never ran and
`container` never reached its push step — there is no existing
Release/image tag to conflict with.

**Do not** re-push a tag without deleting it first — `git push` will
simply reject a non-fast-forward tag update, which is correct behaviour,
not a bug to work around.

**`container` succeeds (image pushed to GHCR) but `publish` subsequently
fails (no GitHub Release is created):**
This is the one genuinely non-atomic case, and it is one-directional only
— see above for why a GitHub Release can never exist without the
matching GHCR image already having been pushed first. Recovery:

- Delete the container tag(s) from GHCR for that version (via the package
  settings in the GitHub UI, or `gh api --method DELETE` against the
  package version), delete the Git tag, and re-tag. The workflow's own
  existence checks (both `container`'s "refuse to overwrite an
  already-published exact version tag" step and `publish`'s own release
  check) are what would otherwise block a clean re-run — clearing the
  GHCR side is required before re-tagging.
- Do not try to manually create the missing GitHub Release against the
  already-pushed image as a shortcut — re-running the whole pipeline from
  a clean tag is simpler and leaves no ambiguity about which native
  artifacts/checksums actually correspond to that published image.

**A bad release was fully published (both GitHub Release and GHCR image
exist, but something is wrong with the content — e.g. the wrong commit was
tagged):**
The workflow's immutability guarantee is deliberate here: it will not let
you silently overwrite `v1.2.3` with different content, because that would
make the tag ambiguous for anyone who already pulled it. Instead:

1. Mark the GitHub Release as unpublished/draft or add a release note
   pointing to the corrected version — don't delete it outright if anyone
   may already have downloaded from it, since deleting a Release someone
   has already referenced (a download link, a Dockerfile's `FROM`,
   internal documentation) is worse than leaving a clearly-marked bad one
   in place.
2. Cut a new, correct patch release (`v1.2.4`) through the normal
   procedure. Do not attempt to reuse or mutate `v1.2.3`.

**A GitHub Actions job produces confusing or contradictory logs:**
The public GitHub API's job-log endpoint (`/repos/.../actions/jobs/{id}/logs`)
is not readable without a token with admin rights on the repository — this
was hit during this exact audit. Use the job's status (success/failure)
and step names from the Actions UI/API instead, and treat the workflow's
own fail-closed design (above) as the guarantee, rather than relying on
being able to read raw logs from outside the UI.

## Failure-mode review (broader than just the release workflow)

| Scenario | Behaviour |
| --- | --- |
| `DATABASE_URL` missing, `APP_ENV=production` | Startup fails immediately with a clear, credential-free error. No silent fallback. |
| Wrong DB password / unreachable PostgreSQL | Startup fails at the first connection attempt (or `/health/db` reports unready if the app is already up and Postgres becomes unreachable later); never a silent degraded mode. |
| A migration fails on startup | The application does not start serving traffic; the failure is logged and the process exits non-zero, so a container orchestrator's own restart/backoff policy applies rather than the app pretending to be healthy. |
| Port already in use | The process fails to bind and exits immediately with the OS-level bind error. |
| Container crashes | `restart: unless-stopped` (Compose) restarts it; PostgreSQL's data survives in its named volume regardless. |
| `SIGTERM` (container stop, Compose shutdown) | Graceful shutdown (Milestone 9): in-flight requests get up to 5 seconds to finish before the process exits; `stop_grace_period: 15s` in `compose.prod.yaml` is set comfortably longer so Compose's own SIGKILL never cuts this off early. |
| Host reboot | Named volume (`postgres_prod_data`) persists; `restart: unless-stopped` brings both services back up once Docker itself restarts. |
| Accidental `docker compose down -v` | **Destructive and irreversible** without a separate backup — this is why the README calls it out with an explicit warning rather than treating it as an ordinary command. |
| Invalid release tag format | Caught by the `validate` job before any build starts (see above). |
| Dirty working tree at release time | `cmd/release` does not fail — it warns, since the embedded commit always refers to `HEAD`, never uncommitted changes; the release still accurately reflects what's on that commit. |
| Duplicate GitHub Release / duplicate exact GHCR tag | `publish` refuses to proceed — see the workflow's own immutability check above. |
| GHCR push succeeds, GitHub Release creation subsequently fails | See "Recovery procedures" above — the one genuinely non-atomic case. Architecturally one-directional: a GitHub Release can never exist without the GHCR image already having been pushed first (`publish` depends on `container`, which is what pushes to GHCR). |
| A bad release is fully published | See "Recovery procedures" above — cut a new patch version; never mutate a published tag. |
