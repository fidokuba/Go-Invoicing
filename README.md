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

## Operations

Basic observability, added in Milestone 10.

- **Logs**: structured (`log/slog`) on stdout, in the format/level `LOG_FORMAT`/`LOG_LEVEL` configure. A single startup log event reports build metadata (version/commit/build time); every request gets a server-generated `X-Request-ID` (a client-supplied one is ignored), returned in the response header and included in that request's log lines. Successful `/health` and `/health/db` checks and `/metrics` scrapes are kept out of the normal per-request INFO log to avoid noise; failures still log.
- **`/health`**: liveness — the process is up. **`/health/db`**: readiness — the database is reachable. Neither requires authentication.
- **`/metrics`**: Prometheus exposition (HTTP request/duration counters, session-cleanup worker and PDF-generation metrics, DB pool state, build-info, standard Go/process stats). Controlled by `METRICS_ENABLED` (default `true`); when `false`, the route doesn't exist at all (plain 404), rather than returning a "disabled" response. It requires no authentication of its own and is **not** part of the `/api/v1` contract or `api/openapi.yaml`.
- **Deployment note**: `/metrics` should be network-restricted (reverse proxy / firewall / private network) in any real deployment — this application does not implement its own access control for it. That's a Milestone 11 deployment concern, not something the application enforces today.
- **No business data in observability**: logs and metrics never contain request bodies, tokens, passwords, emails, `DATABASE_URL`, or any tenant/customer/invoice identifier — only bounded values (HTTP method, route pattern, status code, worker/result names, version/commit). Metric labels in particular are deliberately low-cardinality; see `internal/metrics` for the full inventory.
