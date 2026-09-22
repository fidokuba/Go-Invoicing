| Milestone | What we'll build                         | Main Go/backend skills                             |
| --------- | ---------------------------------------- | -------------------------------------------------- |
| **1**     | Project + PostgreSQL + basic HTTP server | Go tooling, modules, HTTP, config, Docker          |
| **2**     | Customers API                            | Handlers, routing, JSON, validation                |
| **3**     | Products/services API                    | Repository pattern, PostgreSQL, pgx                |
| **4**     | Invoice creation                         | Transactions, business logic, SQL                  |
| **5**     | Invoice lifecycle                        | State transitions, validation                      |
| **6**     | Payments                                 | Financial/business logic                           |
| **7**     | Reports                                  | SQL aggregation and API design                     |
| **8**     | Testing                                  | Unit, integration and database tests               |
| **9**     | Error handling + logging                 | Production-style API engineering                   |
| **10**    | Authentication                           | Middleware, password hashing, JWT/session concepts |
| **11**    | PDF invoices                             | File generation and HTTP responses                 |
| **12**    | Emailing                                 | External services and background work              |
| **13**    | Background workers                       | Goroutines, channels, graceful shutdown            |
| **14**    | Dockerised application                   | Multi-stage builds                                 |
| **15**    | GitHub Actions                           | CI, test/build/lint                                |
| **16**    | Advanced architecture                    | Concurrency, queues, potential service separation  |

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

**Exposure**: `compose.prod.yaml` publishes the app directly on port 8080 (plain HTTP) and PostgreSQL on no host port at all (reachable only from `app`, over the Compose network). Direct port 8080 publication is appropriate for a trusted LAN or an evaluation deployment — it is **not** internet-production-ready as-is. Anything internet-facing needs a reverse proxy/TLS terminator in front (Caddy, nginx, a cloud load balancer, ...); this Part deliberately doesn't add one — see Milestone 11 Part 7 for hardening the final topology. `METRICS_ENABLED` defaults to `false` in this file specifically, because there's no reverse proxy yet to keep `/metrics` off whatever publishes port 8080; turn it on only once there's an actual monitoring consumer and a network boundary for it.

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

`.github/workflows/ci.yml` runs on every pull request and every push to `main`, as four jobs:

- **quality** — `gofmt`, `go vet`, `go build`, a `go mod tidy` cleanliness check, the non-database test suite, and a smoke test of the canonical release build tool (below) across all five release targets, with the resulting artifacts discarded. No database involved.
- **integration** — the complete test suite, including every PostgreSQL-backed test, against a real, ephemeral `postgres:18` service container (disposable CI-only credentials, health-checked before anything runs).
- **race** — the same complete suite again under `-race`.
- **docker** — builds the production image (`Dockerfile`) with throwaway metadata, then boots the real container against its own dedicated ephemeral PostgreSQL service and polls `/health`/`/health/db` over HTTP, proving the actual container startup path (including its real startup migrations) works — not just that `docker build` succeeds. Never pushes or publishes the image.

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
