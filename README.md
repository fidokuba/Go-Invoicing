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

## Operations

Basic observability, added in Milestone 10.

- **Logs**: structured (`log/slog`) on stdout. Every request gets a server-generated `X-Request-ID` (a client-supplied one is ignored), returned in the response header and included in that request's log lines. Successful `/health` and `/health/db` checks and `/metrics` scrapes are kept out of the normal per-request INFO log to avoid noise; failures still log.
- **`/health`**: liveness — the process is up. **`/health/db`**: readiness — the database is reachable. Neither requires authentication.
- **`/metrics`**: Prometheus exposition (HTTP request/duration counters, session-cleanup worker and PDF-generation metrics, DB pool state, standard Go/process stats). Controlled by `METRICS_ENABLED` (default `true`); when `false`, the route doesn't exist at all (plain 404), rather than returning a "disabled" response. It requires no authentication of its own and is **not** part of the `/api/v1` contract or `api/openapi.yaml`.
- **Deployment note**: `/metrics` should be network-restricted (reverse proxy / firewall / private network) in any real deployment — this application does not implement its own access control for it. That's a Milestone 11 deployment concern, not something the application enforces today.
- **No business data in observability**: logs and metrics never contain request bodies, tokens, passwords, emails, or any tenant/customer/invoice identifier — only bounded values (HTTP method, route pattern, status code, worker/result names). Metric labels in particular are deliberately low-cardinality; see `internal/metrics` for the full inventory.
