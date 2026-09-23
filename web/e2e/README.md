# End-to-end tests

`workflow.spec.ts` drives the real, built frontend against a real Go API
and a real PostgreSQL database — nothing about the backend is mocked
(Milestone 12 section 39).

## Running locally

1. Start PostgreSQL (the same throwaway dev database `compose.yaml`
   provides works fine):

   ```bash
   docker compose up -d postgres
   ```

2. Build the frontend into the Go binary's embedded assets, then build
   and run the API from the repository root:

   ```bash
   cd web && npm ci && npm run build
   cd .. && go run ./cmd/api
   ```

   (`APP_ENV` defaults to `development`, which uses the same local
   database URL `compose.yaml` provisions — see `internal/config`.)

3. In another terminal, run the suite:

   ```bash
   cd web
   npx playwright install --with-deps chromium   # first run only
   npm run e2e
   ```

`playwright.config.ts` targets `http://localhost:8080` by default
(override with `E2E_BASE_URL`) and does not start its own server — the
target is the real embedded-frontend Go binary, not something npm can
launch on its own.

## What it covers

Registration → login → session restoration after refresh → organisation
details → customer → product → invoice (using the product to help
populate a line, which stays editable) → send → PDF (verified via the
authenticated API response directly, alongside exercising the View PDF
button for the absence of an error) → payment → Paid → logout → the
dashboard becoming unreachable again.

Each run generates a fresh organisation/email so it can be re-run
against a persistent (non-ephemeral) database without unique-constraint
collisions.

## CI

`.github/workflows/ci.yml`'s `e2e` job runs this automatically against
an ephemeral PostgreSQL service container and the production-built,
frontend-embedded binary.
