| Milestone | What it built                                    | Status |
| --------- | ------------------------------------------------- | ------ |
| **1**     | Project & database foundations (Go tooling, PostgreSQL, config, basic HTTP server) | Done |
| **2**     | Application architecture & layering (handlers/services/repositories) | Done |
| **3**     | Core invoicing (customers, products, invoices, transactions) | Done |
| **4**     | Authentication, authorization & tenant security (Argon2id password hashing, login, opaque PostgreSQL-backed sessions, Bearer auth, admin/manager/user roles, tenant isolation) | Done |
| **5**     | Invoice lifecycle (state transitions, immutable party snapshots on Send) | Done |
| **6**     | Background processing (session-cleanup worker, graceful shutdown) | Done |
| **7**     | Invoice documents / PDF generation (embedded-font rendering, pagination, payment summary, `GET /invoices/{id}/pdf`) | Done |
| **8**     | API quality & OpenAPI (`api/openapi.yaml`, contract tests) | Done |
| **9**     | Reliability & advanced testing (error handling, structured logging, race/DB test coverage) | Done |
| **10**    | Observability (structured logs, `/health`, `/health/db`, Prometheus `/metrics`) | Done |
| **11**    | CI/CD & deployment (Docker, Docker Compose, GitHub Actions CI, `cmd/release`, tagged-release workflow publishing to GHCR) | Done |
| **12**    | Frontend & usable application (React/TypeScript/Vite browser app, embedded into the production Go binary) | Done |
| **13**    | Advanced backend engineering (13.1: idempotent, safely retryable payment creation via a required `Idempotency-Key`) | In progress |

> **Note:** this table reflects the project's actual roadmap, not the original teaching-plan draft. Earlier drafts of this README numbered milestones differently (e.g. describing an early "Milestone 10" as authentication and "Milestone 11" as PDF invoices, and at one point swapping Milestones 12/13's own descriptions); those numbers were superseded once the project's real scope diverged from that draft, and this table is the corrected, current source of truth. Emailing (invoice delivery, payment/overdue reminders, scheduled delivery, templates, delivery tracking) was deliberately removed from the core roadmap — it is a potential future/commercial feature, not a gap in this project's own scope. See "Production readiness" below for what "done" through Milestone 12 does and doesn't mean for the complete product.

## Configuration

All configuration is environment variables (see `.env.example` for a documented starting point).

| Variable | Default | Notes |
| --- | --- | --- |
| `APP_ENV` | `development` | Only `development` (case-insensitive) may fall back to the `DATABASE_URL` default below — any other value (`production`, `staging`, ...) requires `DATABASE_URL` to be set, or startup fails with a clear, credential-free error. |
| `APP_HOST` | `` (every interface) | The interface to bind. `127.0.0.1` binds loopback-only; an IPv6 address such as `::1` is bracketed correctly (`net.JoinHostPort`, not string concatenation). |
| `APP_PORT` | `8080` | Must be a number from 1–65535. |
| `DATABASE_URL` | dev-only default (see above) | Never logged, in full or in part, anywhere — including on a malformed-value startup failure. |
| `LOG_FORMAT` | `text` | `text` (development-friendly) or `json` (structured, for a log aggregator). Explicit — never auto-switched based on `APP_ENV`. |
| `LOG_LEVEL` | `info` | `debug`, `info`, `warn`, or `error`. |
| `WORKER_ENABLED` | `true` | Session-cleanup background worker. |
| `WORKER_INTERVAL` | `1h` | Must be a positive duration. |
| `WORKER_BATCH_SIZE` | `500` | Must be a positive integer. |
| `METRICS_ENABLED` | `true` | See `/metrics` below. |

An invalid value for any of the above fails startup immediately with a descriptive error — never a silent fallback to the default.

**Build metadata**: `go run ./cmd/api --version` (or a built binary's own `--version`) prints version/commit/build-time without touching configuration, the database, or starting anything. An ordinary build not produced by a release pipeline reports `dev`/`unknown`/`unknown` — real values are injected at build time via linker flags (`internal/buildinfo`), not read from Git at runtime.

## Frontend

Milestone 12 adds a browser-based application under `/web`: React, TypeScript, Vite, and Tailwind CSS, talking to the existing `/api/v1` REST API with no backend redesign. Frontend engineering is intentionally not this project's primary focus — the dependency set is kept small, and conventional patterns are preferred over cleverness throughout.

**Stack**: React 19 + TypeScript (strict) + Vite + Tailwind CSS v4. Server state (loading/caching/mutations) is handled by TanStack Query; routing by React Router. A handful of accessible unstyled primitives (`@radix-ui/react-dialog`, `@radix-ui/react-dropdown-menu`, `@radix-ui/react-slot`) back a small set of hand-written UI components (button, input, table, badge, dialog, ...) rather than a generated component library — the same approach shadcn/ui popularized, without its CLI/scaffolding step.

**Typed API client**: `web/src/api/schema.d.ts` is generated from `api/openapi.yaml` via `openapi-typescript` (`npm run gen:api`, from `/web`) — the backend's own OpenAPI document is the single source of truth for every request/response type; nothing is hand-typed in a way that could silently drift from it. `openapi-fetch` (a ~1 KB runtime) provides a thin, fully-typed `fetch` wrapper over those types; a small `unwrap()` helper turns its `{data, error}` result into either a value or a typed `ApiError` every query/mutation hook can branch on. This is deliberately lighter than a full client-generation pipeline (no generated SDK, no generated hooks) while still keeping types synchronized with the contract — CI fails if `schema.d.ts` is regenerated and differs from what's committed.

**Authentication/token storage**: the existing opaque Bearer-token API is used as-is — no cookies, no backend redesign. The token and last-known user are kept in `localStorage` (`web/src/lib/authStore.ts`), the simplest browser-storage option, restoring the session automatically on refresh. **Trade-off**: unlike an httpOnly cookie, this token is readable by any JavaScript running on the page, so a successful XSS against this frontend could exfiltrate it — mitigated (not eliminated) by this codebase never using `dangerouslySetInnerHTML` or any other raw-HTML injection point. A 401 from any request clears the local session; a shared `ProtectedRoute` reacts to that and redirects to `/login`, which cannot loop (the login page itself isn't behind that guard). Revisiting this as a cookie-based session is a reasonable candidate for a future milestone, not this one.

**Development** (three processes, same as any Vite-in-front-of-an-API setup):

```sh
# 1. Database
docker compose up -d postgres

# 2. Go API (from the repository root)
go run ./cmd/api

# 3. Frontend dev server (from web/)
cd web
npm install
npm run dev          # http://localhost:5173
```

The Vite dev server proxies `/api/*` and `/health*` to `http://localhost:8080` (see `web/vite.config.ts`), so the browser only ever talks to one origin in development — **no CORS configuration exists anywhere in this project**, in development or production, because neither setup ever needs it.

**Frontend commands** (run from `web/`):

| Command | Purpose |
| --- | --- |
| `npm run dev` | Vite dev server with the API proxy above. |
| `npm run build` | Production build, written directly into `../internal/webui/dist` (see "Production architecture" below) — `tsc -b` first, so a type error fails the build. |
| `npm run lint` | ESLint. |
| `npm run typecheck` | `tsc -b --noEmit`. |
| `npm test` | Vitest (unit/component tests). |
| `npm run e2e` | Playwright, against a real running API — see "End-to-end testing" below. |
| `npm run gen:api` | Regenerates `src/api/schema.d.ts` from `api/openapi.yaml`. |

**Production architecture — one binary serves everything:**

```
npm run build (web/)  →  internal/webui/dist  →  go:embed  →  one Go binary
                                                                (API + frontend + migrations + OpenAPI + PDF assets)
```

`internal/webui` embeds `internal/webui/dist` via `go:embed` — the same pattern `api/openapi.go` already used for the OpenAPI document. A minimal placeholder `index.html` is committed there specifically so `go build ./...`/`go test ./...` never require Node — `go:embed` needs *something* on disk at compile time, and this project's Go quality gates must keep working without a frontend toolchain installed. `npm run build` overwrites that placeholder with the real build before the Go binary is compiled for packaging (Docker, `cmd/release`, or a manual `go build ./cmd/api` after building the frontend); `internal/webui/dist`'s contents (other than the placeholder) are git-ignored.

Serving is handled without registering any new route on the API's `net/http.ServeMux` at all: `internal/httpx.FrontendFallback` intercepts only a GET/HEAD request that the router found *no registered pattern for whatsoever*, and hands it to `internal/webui.Serve`, which serves a real static asset, falls back to `index.html` for a recognised client-side route (so a refreshed/deep-linked URL like `/invoices/<id>` renders the SPA), or reproduces net/http's own plain-text 404 for anything else. `/api/v1/*`, `/health`, `/health/db`, and `/metrics` are never affected — every one of them either has its own real route (which always takes precedence) or, if genuinely unmatched, gets the same plain-text 404 it always did, never the frontend shell. (An earlier version of this registered a `GET /` pattern directly on the mux; that was reverted after proving — via this project's own pre-Milestone-12 test suite — that it corrupts `net/http.ServeMux`'s 405-vs-404 distinction for *every other unmatched path in the application*. See `internal/webui`'s and `internal/httpx.FrontendFallback`'s own doc comments for the full explanation.)

Hashed build assets (`/assets/*`, content-hashed filenames from Vite) are served with `Cache-Control: public, max-age=31536000, immutable`; `index.html` and everything else is `no-cache`, so a deployment is never masked by a stale cached shell.

**Browser access**: once running (locally, in Docker, or from a native release binary), the whole application — API and frontend — is reachable at the server's own address, e.g. `http://localhost:8080/`.

**Docker/native releases**: the `Dockerfile` gained a `frontend-builder` stage (`node:22-bookworm-slim`, pinned by digest like every other base image here) that runs `npm ci && npm run build` before the existing Go builder stage compiles the binary; the runtime image is unchanged — still distroless, non-root, no shell, and now also **no Node runtime**, since only the already-built static files cross into the Go builder stage, never the frontend source or `node_modules`. `cmd/release`'s five native binaries embed whatever is on disk at `internal/webui/dist` when it runs — the release workflow (`.github/workflows/release.yml`) builds the frontend first; a local `go run ./cmd/release` without doing so still works, but prints a warning (mirroring the existing dirty-git-tree warning) rather than silently shipping the placeholder.

**Frontend testing**: Vitest + Testing Library, focused on behaviour over coverage percentage — money/date conversion and formatting, API error mapping, the auth store's session/persistence behaviour, invoice-line calculation/serialization, and lifecycle-based action visibility (`web/src/**/*.test.ts(x)`).

**End-to-end testing**: Playwright (`web/e2e/workflow.spec.ts`) drives the real built frontend against a real Go API and real PostgreSQL — nothing about the backend is mocked. It covers registration → login → session restoration after refresh → organisation details → customer → product → invoice (using a product to help populate a line, which stays editable) → send → PDF (verified via the authenticated API response directly, since asserting on a browser-native PDF viewer's contents isn't practical) → payment (including a lost-response retry that must replay the already-recorded payment via its `Idempotency-Key`, not record a second one) → Paid → logout. See `web/e2e/README.md` for exactly how to run it locally; CI runs it automatically in its own job.

## Release builds

**Ordinary development build** — no version required, uses `internal/buildinfo`'s `dev`/`unknown` defaults:

```sh
go build ./cmd/api
# or
go run ./cmd/api
```

**Canonical release build** — cross-compiles all five supported platforms (`linux/amd64`, `linux/arm64`, `windows/amd64`, `darwin/amd64`, `darwin/arm64`) with `CGO_ENABLED=0`, injects version/commit/build-time, strips debug symbols (`-s -w`), and writes a `checksums.txt`:

```sh
go run ./cmd/release -version v1.2.3
```

`-version` is **required** and must be a fully-specified semantic version with a leading `v` (`v1.2.3`, or `v1.2.3-rc.1` for a prerelease) — the build refuses to run without one, so a release can never accidentally ship with `internal/buildinfo`'s `dev` placeholder. Commit defaults to `git rev-parse HEAD` (the full SHA) and build time defaults to the current UTC time in RFC 3339 — both can be overridden (`-commit`, `-build-time`) so CI can supply one deterministic value instead of every invocation minting its own; given the same explicit version/commit/build-time, the build is byte-for-byte reproducible. A dirty working tree does not fail the build — it prints a warning, since the embedded commit always refers to `HEAD`, not any uncommitted changes.

Output (gitignored, never committed):

```
dist/
  go-invoicing_v1.2.3_linux_amd64
  go-invoicing_v1.2.3_linux_arm64
  go-invoicing_v1.2.3_windows_amd64.exe
  go-invoicing_v1.2.3_darwin_amd64
  go-invoicing_v1.2.3_darwin_arm64
  checksums.txt
```

Verify an artifact against `checksums.txt`:

```sh
cd dist && sha256sum -c checksums.txt   # Linux
cd dist && shasum -a 256 -c checksums.txt   # macOS
```

The Linux/Windows/macOS-amd64/macOS-arm64 artifacts are unsigned and unnotarized — appropriate for development, testing, and technical/self-hosted distribution, not yet a polished non-technical commercial macOS/Windows install experience. Downloading one may trigger an OS trust warning (Windows SmartScreen, macOS Gatekeeper) — this is expected until Authenticode/notarization exist, and this Part doesn't pretend otherwise.

The release build only compiles — it never opens a database connection, runs a migration, or reads application configuration, and needs no secret of any kind.

## Releases

`.github/workflows/release.yml` (Milestone 11 Part 6) turns a Git tag into a published GitHub Release and a multi-arch container image — a separate workflow from CI, triggered only by a tag, never by an ordinary push or pull request.

**Tag convention**: `vMAJOR.MINOR.PATCH`, optionally with a prerelease suffix — `v1.2.3`, `v1.2.3-rc.1`. Validated with the exact same rule `cmd/release` itself enforces (via its `-validate-only` flag) — an invalid tag name fails the workflow before anything is built.

**What gets published, for tag `v1.2.3`:**

- A GitHub Release named `Go Invoicing v1.2.3`, with GitHub's auto-generated notes, and all six `cmd/release` outputs attached: the five native binaries plus `checksums.txt`.
- A multi-arch (`linux/amd64` + `linux/arm64`) image on GHCR, tagged `ghcr.io/<owner>/go-invoicing:v1.2.3`, `:1.2.3`, `:1.2`, `:1`, and `:latest`.

**Prerelease tags** (`v1.2.3-rc.1`) publish a GitHub Release marked as a prerelease, and a container tagged only `:v1.2.3-rc.1` and `:1.2.3-rc.1` — never `:latest`, and never a `:1.2`/`:1` alias, so a prerelease can never accidentally become what `docker pull .../go-invoicing` resolves to by default.

**Native binaries remain unsigned** (see "Release builds" above) — the same OS trust-warning caveat applies to a release download as to a local `cmd/release` build.

**Nothing is ever silently overwritten**: the workflow refuses to proceed if a GitHub Release or the exact container version tag already exists for that version, rather than replacing it.

**Publishing a release never deploys it anywhere** — no server, container host, or cloud environment is touched by this workflow. It only makes artifacts available for someone to deploy manually (see the "Self-hosted Docker Compose"/"Cloud" sections above).

**Operator procedure** (not automated — a human decides when to cut a release):

1. Confirm CI is green on the commit you intend to release.
2. `git tag v1.2.3` (an annotated tag, `git tag -a v1.2.3 -m "..."`, works equally well).
3. `git push origin v1.2.3`.
4. Watch the "Release" workflow run in the Actions tab.
5. Once it succeeds, verify the GitHub Release page and (if you use the image) `docker pull ghcr.io/<owner>/go-invoicing:v1.2.3`.

If a release run fails partway through, or you need to correct a bad tag, see [`docs/releases.md`](docs/releases.md) for the recovery procedure — do not just re-push the same tag.

**Supply-chain hardening — explicit decisions (Milestone 11 Part 7):**

- **Base images**: the Dockerfile's `golang` and `distroless` base images are pinned by digest, not floating tag, for build reproducibility; `.github/dependabot.yml` keeps those digests current automatically. `postgres:18` (Compose) is deliberately left on a floating major-version tag — see the Dockerfile's and `.github/dependabot.yml`'s own comments for why.
- **GitHub Actions**: pinned by version tag (`@v7`, etc.), kept current by Dependabot's `github-actions` ecosystem — not pinned by commit SHA. SHA-pinning without an update bot just freezes a workflow forever; a version tag plus Dependabot gets the same supply-chain benefit (an auditable, reviewed PR for every change) without that risk.
- **SBOM**: not generated. Deferred — no consumer of one exists yet (no enterprise/compliance customer, no internal vulnerability-management pipeline ingesting it), and generating one nobody reads is complexity without benefit. Revisit if a real consumer appears.
- **Container signing (Cosign/sigstore)**: not implemented. Deferred for the same reason as SBOM — signature verification only has value once something downstream actually checks it (e.g. an admission controller), and nothing in this project's current deployment story (Compose, a single cloud container) does. `checksums.txt` plus GitHub's own release/commit provenance is the current integrity story for native binaries; the GHCR image's integrity relies on GHCR/registry TLS and the `@sha256:` digest a puller can pin to.
- **SLSA provenance / build attestations**: not implemented, for the same reason — no consumer verifies them today, and GitHub Actions' attestation tooling adds real workflow complexity for a project at this stage. The workflow's own source (this public repository) and its immutable Actions run history are the current substitute for "how was this built."
- **Native binary signing (Authenticode/notarization)**: not implemented — see "Release builds" above. This is the one item on this list with a real, felt user-facing cost today (OS trust warnings), but Authenticode/notarization require a paid certificate and Apple Developer Program enrollment respectively, which is a business decision, not an engineering one, and out of scope for this Part.
- **Container vulnerability scanning**: not added as a CI gate. `govulncheck` (Go's own dependency-vulnerability scanner) already runs as part of this audit's dependency review (see below) and catches what actually matters for a `CGO_ENABLED=0` static binary — the Go module graph. A container-layer scanner (Trivy/Grype) would mostly be re-reporting the same base-image CVEs Dependabot's `docker` ecosystem already surfaces through routine base-image bumps; adding a second, redundant scanning gate isn't a genuine gap at this project's current size.

## Operations

Basic observability, added in Milestone 10.

- **Logs**: structured (`log/slog`) on stdout, in the format/level `LOG_FORMAT`/`LOG_LEVEL` configure. A single startup log event reports build metadata (version/commit/build time); every request gets a server-generated `X-Request-ID` (a client-supplied one is ignored), returned in the response header and included in that request's log lines. Successful `/health` and `/health/db` checks and `/metrics` scrapes are kept out of the normal per-request INFO log to avoid noise; failures still log.
- **`/health`**: liveness — the process is up. **`/health/db`**: readiness — the database is reachable. Neither requires authentication.
- **`/metrics`**: Prometheus exposition (HTTP request/duration counters, session-cleanup worker and PDF-generation metrics, DB pool state, build-info, standard Go/process stats). Controlled by `METRICS_ENABLED` (default `true`); when `false`, the route doesn't exist at all (plain 404), rather than returning a "disabled" response. It requires no authentication of its own and is **not** part of the `/api/v1` contract or `api/openapi.yaml`.
- **Deployment note**: `/metrics` should be network-restricted (reverse proxy / firewall / private network) in any real deployment — this application does not implement its own access control for it. That's a Milestone 11 deployment concern, not something the application enforces today.
- **No business data in observability**: logs and metrics never contain request bodies, tokens, passwords, emails, `DATABASE_URL`, or any tenant/customer/invoice identifier — only bounded values (HTTP method, route pattern, status code, worker/result names, version/commit). Metric labels in particular are deliberately low-cardinality; see `internal/metrics` for the full inventory.

## Self-hosted Docker Compose

`compose.prod.yaml` (Milestone 11 Part 5) runs Go Invoicing plus its own PostgreSQL, for a self-hosted/customer deployment — a **separate file** from the development-only `compose.yaml` (which only ever provisions a local Postgres for a natively-run `go run ./cmd/api`; that workflow is unchanged). Always pass `-f compose.prod.yaml` explicitly.

**Prerequisites**: Docker with Compose v2 (`docker compose`, not the standalone `docker-compose`).

**Production env setup** — copy the template and fill in your own strong, unique values (`DATABASE_URL` must describe the same database as the `POSTGRES_*` values — see the file's own comment on why these aren't constructed for you):

```sh
cp .env.production.example .env.production
# edit .env.production
```

**Start** (builds the image locally; add `--build` again after pulling a code update):

```sh
docker compose -f compose.prod.yaml --env-file .env.production up -d --build
```

**Logs**:

```sh
docker compose -f compose.prod.yaml logs -f app
```

**Health** (the image ships no `HEALTHCHECK` — it's built on a shell-less distroless base; check over HTTP instead, from outside the container):

```sh
curl -f http://localhost:8080/health
curl -f http://localhost:8080/health/db
```

**Stop**:

```sh
docker compose -f compose.prod.yaml stop        # keeps containers/data, for a later restart
docker compose -f compose.prod.yaml down         # removes containers, KEEPS the named volume/data
```

**Data persistence**: PostgreSQL's data lives in the named volume `postgres_prod_data`, which survives container recreation, image upgrades, and host reboots.

> ⚠️ **`docker compose -f compose.prod.yaml down -v` permanently deletes the database volume.** Never run this against a deployment with real data unless you have a verified backup and genuinely intend to discard everything.

**Backup** (`pg_dump`, this stage's whole backup story — no in-app scheduler, no automatic sidecar):

```sh
docker compose -f compose.prod.yaml exec postgres \
  pg_dump -U <POSTGRES_USER> -d <POSTGRES_DB> | gzip > backup-$(date +%Y%m%d-%H%M%S).sql.gz
```

**Restore** (into a fresh/empty database — never automated, since it's destructive):

```sh
gunzip -c backup-YYYYMMDD-HHMMSS.sql.gz | \
  docker compose -f compose.prod.yaml exec -T postgres psql -U <POSTGRES_USER> -d <POSTGRES_DB>
```

A backup taken on an older application version restores safely under a *newer* one (migrations run forward on the next startup); restoring it under an *older* version than it was taken from is not guaranteed safe (see Milestone 11 Part 1's own restore-architecture note).

**Upgrade** (conceptually — not automated by anything in this repository yet):

1. Back up (above).
2. Pull/build the new image (`docker compose -f compose.prod.yaml pull` or `--build` with updated source).
3. `docker compose -f compose.prod.yaml up -d` recreates only the `app` container; PostgreSQL and its volume are untouched.
4. The new container runs its own startup migrations automatically (see cmd/api/main.go) before it starts serving.
5. Verify with the health checks above.

**Exposure**: `compose.prod.yaml` publishes the app directly on port 8080 (plain HTTP) and PostgreSQL on no host port at all (reachable only from `app`, over the Compose network). Direct port 8080 publication is appropriate for a trusted LAN or an evaluation deployment — it is **not** internet-production-ready as-is. Anything internet-facing needs a reverse proxy/TLS terminator in front (Caddy, nginx, a cloud load balancer, ...) — this remains a deliberate, permanent operator/deployment-topology decision, not something this project's own Compose file should choose on every self-hoster's behalf. `METRICS_ENABLED` defaults to `false` in this file specifically, because there's no reverse proxy in front to keep `/metrics` off whatever publishes port 8080; turn it on only once there's an actual monitoring consumer and a network boundary for it.

**Horizontal scaling boundary**: this Compose file assumes exactly one `app` instance. It performs its own startup migrations on every start (fine for one instance; see Milestone 11 Part 1's own note on why concurrent migration races become a real concern once there's more than one). Running multiple `app` replicas against the same database is not supported by this Part — that needs migrations to become an explicit, separate deployment step first.

## Cloud

The recommended shape (no vendor chosen — this is architecture, not a specific provider's setup):

```
Internet → managed TLS / load balancer / PaaS edge → Go Invoicing container → managed PostgreSQL
```

- **Container**: the same `Dockerfile` this Part adds — the app container is entirely stateless, so it can be replaced/recreated/scaled by whatever the platform's own deploy mechanism is.
- **Database**: a **managed** PostgreSQL service, not a container colocated with the app — backups, patching, storage durability, monitoring, and HA are exactly the operational burdens a managed offering exists to absorb (see Milestone 11 Part 1's own comparison). PostgreSQL never runs inside the application container.
- **Config/secrets**: conceptually — `APP_ENV=production`, `APP_HOST` left empty (every interface), `APP_PORT` per the platform's own convention (some PaaS platforms inject their own `PORT` variable rather than reading `APP_PORT` — no automatic compatibility shim exists for this yet; map it explicitly for whichever platform is eventually chosen), `DATABASE_URL` from the platform's secret storage, `LOG_FORMAT=json`, `LOG_LEVEL=info`, worker settings per normal defaults, `METRICS_ENABLED` according to whether real monitoring infrastructure exists yet. No provider-specific config file is committed here.
- **TLS**: terminates at the load balancer/PaaS edge, never inside the Go process — it continues speaking plain HTTP internally, exactly as it does today.
- **Health**: `/health` (liveness) and `/health/db` (readiness) as separate checks, wherever the platform supports distinguishing them.
- **Metrics**: `/metrics`, if enabled at all, should only be reachable from private monitoring infrastructure — never through the same public ingress as the API. No Prometheus deployment is included here.
- **Logs**: structured JSON to stdout (`LOG_FORMAT=json`), captured by whatever the platform's own logging pipeline is — no file logs, no log shipper added by this application.

## CI

`.github/workflows/ci.yml` runs on every pull request and every push to `main`, as eight jobs:

- **quality** — `gofmt`, `go vet`, `go build`, a `go mod tidy` cleanliness check, the non-database test suite, and a smoke test of the canonical release build tool (below) across all five release targets, with the resulting artifacts discarded. No database, and no Node — this job only ever embeds the committed `internal/webui/dist` placeholder, exactly as it did before Milestone 12.
- **integration** — the complete test suite, including every PostgreSQL-backed test, against a real, ephemeral `postgres:18` service container (disposable CI-only credentials, health-checked before anything runs).
- **race** — the same complete suite again under `-race`.
- **frontend** (Milestone 12) — the frontend's own toolchain, no Go/database involved: frozen `npm ci`, a freshness check on the generated OpenAPI types, lint, type-check, unit/component tests, production build, and a production-dependency vulnerability audit (`npm audit --omit=dev`).
- **embedded** (Milestone 12) — builds the *real* frontend, compiles the Go binary with it embedded, boots it against a real PostgreSQL service, and verifies the frontend is actually served (not the placeholder), a hashed asset gets long-lived caching, `/api/v1`/`/health`/`/metrics` are unaffected, a client-side deep link (e.g. `/invoices/<id>`) renders the SPA, and an unmatched API path never returns HTML. Also runs `cmd/release` with the real frontend present and confirms no "placeholder" warning appears.
- **e2e** (Milestone 12) — the Playwright workflow (`web/e2e/workflow.spec.ts`) against the real built frontend, a real compiled binary, and a real PostgreSQL service.
- **docker** — builds the production image (`Dockerfile`, including its frontend-builder stage) with throwaway metadata, then boots the real container against its own dedicated ephemeral PostgreSQL service and polls `/health`/`/health/db` over HTTP, proving the actual container startup path (including its real startup migrations) works — not just that `docker build` succeeds. Also verifies the embedded frontend is served and that the runtime image contains no Node runtime binary. Never pushes or publishes the image.
- **compose** — validates `compose.prod.yaml` with real `docker compose` (config rendering, required-variable enforcement), then starts the full stack with disposable test credentials and verifies health, the embedded frontend, `/metrics` staying disabled, the container's security settings (non-root, read-only, dropped capabilities), and that data survives a `down`/`up` cycle. Torn down (including its own disposable volume) at the end of the job.

**PostgreSQL-backed tests are mandatory in CI, not best-effort.** Locally, without `DATABASE_URL` set, those tests skip (as they always have) — that's expected and fine for day-to-day development. In CI, `REQUIRE_DATABASE_TESTS=true` makes a dedicated preflight test (`internal/database/ci_preflight_test.go`) fail the build outright if the database is missing or unreachable, so CI can never go green because every DB test silently skipped. That same preflight step also applies migrations for real before the suite runs.

**Why `-p 1`**: the PostgreSQL-backed tests share one database/schema across packages (there is no per-test schema isolation yet), so they must run serially — running packages concurrently against the same live tables is a known, accepted reliability trade-off, not an oversight. The non-database suite has no such constraint and runs with Go's normal default parallelism.

**Reproducing CI locally:**

```sh
# Without PostgreSQL — same as local development; DB-backed tests skip.
go test ./... -count=1

# With PostgreSQL (e.g. `docker compose up -d` for compose.yaml's dev
# database) — the complete suite, serially:
DATABASE_URL=postgres://go_invoicing:go_invoicing_dev@localhost:5432/go_invoicing?sslmode=disable \
  go test -p 1 ./... -count=1

# CI-required mode — fails instead of skipping if the database above
# isn't actually reachable:
REQUIRE_DATABASE_TESTS=true DATABASE_URL=postgres://go_invoicing:go_invoicing_dev@localhost:5432/go_invoicing?sslmode=disable \
  go test ./internal/database/ -run TestRequireDatabaseTests_CIPreflight -v -count=1
```

## Security boundaries

What this project provides, and what it deliberately leaves to an operator or a future milestone. Not a claim that anything unlisted is unsafe — only that it isn't this project's job yet.

**Provided today:**

- Application-level authentication and authorization (Milestone 4): Argon2id password hashing, login, opaque PostgreSQL-backed sessions, Bearer authentication, admin/manager/user role-based authorization, tenant resolution and tenant-isolated data access enforced at the repository layer (`internal/administration`, `internal/invoice/repository_tenant_isolation_test.go`). Every business endpoint under `/api/v1` requires a valid session via `authMiddleware.RequireAuth` (see `internal/app/app.go`); only login/registration/health/metrics are unauthenticated, as is conventional.
- Non-root container (`USER 65532:65532`), minimal distroless runtime image (no shell, no package manager), read-only root filesystem and dropped Linux capabilities in `compose.prod.yaml`.
- Secrets (`DATABASE_URL`, `POSTGRES_PASSWORD`, ...) supplied only at container start from an untracked `.env.production`; never baked into an image or committed (`.gitignore`/`.dockerignore`).
- `DATABASE_URL` never logged, in full or in part, including on a malformed-value startup failure.
- Structured logs/metrics contain no request bodies, tokens, passwords, or tenant/customer/invoice data (see "Operations" above).
- `/metrics` is disabled by default in the self-hosted Compose file specifically because there's no reverse proxy in front of it yet.
- Checksummed native release artifacts (`checksums.txt`), an immutable release process (never overwrites an existing tag/GitHub Release/container tag), and release identity tied directly to a Git commit/tag.
- Base container images pinned by digest with automated (Dependabot) currency; GitHub Actions dependencies and Go module dependencies likewise kept current by Dependabot.
- Least-privilege GitHub Actions workflow permissions (`contents: read` by default; `packages: write`/`contents: write` granted only to the specific jobs that need them).

**Not provided (by design, at this project's current stage):**

- Frontend authentication uses a `localStorage`-held bearer token, not an httpOnly cookie — see "Frontend" above for the trade-off and why this milestone deliberately doesn't redesign backend authentication to change it.
- No email delivery (invoice emailing, payment/overdue reminders, scheduled delivery, templates, delivery tracking) — deliberately deferred, a potential future/commercial feature rather than a core-roadmap gap (see the milestone-table note above).
- No native binary signing (Authenticode/notarization) — see "Supply-chain hardening" above.
- No container image signing, SBOM, or build provenance/attestation — see the same section.
- No secrets manager integration (Vault, cloud KMS, ...) — secrets are plain environment variables, as is conventional for a container at this scale.
- No WAF, rate limiting, or DDoS protection — expected to come from whatever sits in front (reverse proxy, cloud load balancer), not this application.
- No automated backups — `pg_dump`/restore is a documented manual procedure (see "Self-hosted Docker Compose" above), not a scheduled job.
- No horizontal-scaling support — see the "Horizontal scaling boundary" note above; the application performs its own startup migrations, which is only safe with exactly one instance.

## Production readiness

Deliberately **not** a single "production ready: yes/no" claim — that question means different things depending on which of the following is being asked. Each is classified independently, and a higher letter is not "more done" than a lower one; they're different products.

| # | Scope | Status | Notes |
| - | --- | --- | --- |
| **A** | Backend application runtime (the Go process itself: authentication, authorization, tenant isolation, invoice lifecycle, PDF generation, HTTP handlers, business logic, DB access, graceful shutdown, logging, metrics) | **Ready**, for what it currently implements | Full engineering fundamentals (tests, race detection, structured errors, observability) *and* a functionally complete core feature set: real login/session auth, role-based authorization, tenant-isolated data, invoice lifecycle, and PDF invoice generation. Email delivery is deliberately out of the core roadmap (see the milestone-table note above), not a missing backend capability. |
| **B** | Self-hosted technical deployment (an operator who can run `docker compose`, edit `.env` files, and read this README) | **Ready** | `compose.prod.yaml` plus this README's walkthrough is a complete, workable path for a technically competent self-hoster on a LAN or behind their own reverse proxy — including its now-embedded browser frontend. |
| **C** | Cloud container deployment (a managed PaaS/container platform + managed PostgreSQL) | **Architecturally ready, not yet executed** | The "Cloud" section above describes a coherent target shape and the app image is stateless and cloud-portable, but no specific provider has actually been deployed to or verified end-to-end (deliberately out of scope — see this Part's prohibitions). |
| **D** | Native Windows/macOS/Linux server binary (an operator runs the binary and reaches it through a browser — not a signed desktop GUI application) | **Technically runnable, not a polished signed desktop app** | Each of the five release binaries serves the complete application (API + frontend) with no separate download — but they remain unsigned/unnotarized (OS trust warnings), with no installer, no OS service wrapper, and no auto-update. This requires real business decisions (code-signing certificate, Apple Developer Program enrollment) not made here. There is deliberately no Electron/Tauri/Wails wrapper — this is server software, reached over HTTP, on every platform. |
| **E** | Complete end-user invoicing product (something a small business could actually adopt and rely on) | **A first usable version now exists — an early release candidate, not a polished commercial product** | Milestone 12 adds a usable browser application (dashboard, customers, products, invoices with lines/lifecycle/payments/PDF, settings, user management) on top of the existing backend, in one deployable binary/image. Email delivery and related commercial polish (reminders, scheduled delivery, templates, delivery tracking) remain deliberately deferred, and the frontend itself has known rough edges (see the milestone's own report for deferred polish) — this is now a completeness/polish gap, not a missing-frontend gap. |
