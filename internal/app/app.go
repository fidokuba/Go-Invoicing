package app

import (
	"log/slog"
	"net/http"
	"time"

	"go-invoicing/api"
	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/httpx"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/metrics"
	"go-invoicing/internal/product"
	"go-invoicing/internal/ratelimit"
	"go-invoicing/internal/webui"

	"github.com/jackc/pgx/v5/pgxpool"
)

// apiV1Prefix is prepended to every versioned application route
// (Milestone 8 Part 2). /health and /health/db are deliberately excluded
// — they are infrastructure-level probes, not part of the versioned
// application contract, and must keep working regardless of API version.
const apiV1Prefix = "/api/v1"

type App struct {
	db      *pgxpool.Pool
	logger  *slog.Logger
	metrics *metrics.Metrics

	// routes is populated by Handler() as it registers each route — see
	// RoutePattern's own doc comment for why this exists and how tests
	// use it.
	routes []RoutePattern

	// rateLimits (Milestone 13 Part 4) is defaultRateLimits in production;
	// tests in this package may relax it (every httptest request shares
	// one client address).
	rateLimits rateLimits
}

// rateLimitPolicy is one in-process token bucket per key: a sustained
// rate of one request per `every`, with bursts of up to `burst`.
type rateLimitPolicy struct {
	every time.Duration
	burst int
}

type rateLimits struct {
	login    rateLimitPolicy
	register rateLimitPolicy
	pdf      rateLimitPolicy
}

// defaultRateLimits protects the only three endpoints where repeated
// requests are a realistic abuse or resource risk (Milestone 13 Part 4);
// ordinary authenticated CRUD is deliberately not limited.
//
//   - login, per client address: 10 attempts at once, then 1 every 6s
//     (10/min). Ample for a person mistyping a password; slows online
//     password guessing, and bounds the Argon2id hashing each attempt
//     costs the server.
//   - register, per client address: 5 at once, then 1 every 2 minutes.
//     A real organisation registers once; this stops a script filling the
//     database with organisations/users (each also an Argon2id hash).
//   - PDF, per authenticated user: 20 at once, then 1 every 2s. PDF
//     generation is synchronous and CPU-bound (data assembly plus
//     rendering); no human clicks faster, but a looping client could
//     otherwise monopolise the server.
var defaultRateLimits = rateLimits{
	login:    rateLimitPolicy{every: 6 * time.Second, burst: 10},
	register: rateLimitPolicy{every: 2 * time.Minute, burst: 5},
	pdf:      rateLimitPolicy{every: 2 * time.Second, burst: 20},
}

// rateLimitMaxKeys bounds each limiter's memory: at most this many
// per-key buckets (roughly 100 bytes each) — see ratelimit.Limiter.
const rateLimitMaxKeys = 10_000

func (p rateLimitPolicy) newLimiter() *ratelimit.Limiter {
	return ratelimit.New(p.every, p.burst, rateLimitMaxKeys)
}

// New wires an App around an existing database pool, logger, and metrics
// (Milestone 10 Part 4). logger is used only by the panic-recovery
// middleware (see Handler) to record an unexpected handler panic
// server-side before responding with the standard generic 500 — nothing
// else in this package logs anything.
//
// db may be a typed nil *pgxpool.Pool: every repository constructor
// Handler calls below only stores it in a struct field, never dereferences
// it during construction, so building the route table (see RoutePatterns)
// requires no live database connection. A nil pool only becomes a problem
// once a handler that actually queries the database is invoked.
//
// m may be nil, meaning metrics are disabled (see config.Config
// .MetricsEnabled): Handler then never mounts GET /metrics at all (see
// its own comment below on why an absent route, not a disabled-response
// body, is section 32's preferred behaviour), and every *metrics.Metrics
// method used elsewhere in this package's dependency graph is a nil-safe
// no-op, so no other conditional is needed.
func New(db *pgxpool.Pool, logger *slog.Logger, m *metrics.Metrics) *App {
	return &App{db: db, logger: logger, metrics: m, rateLimits: defaultRateLimits}
}

// RoutePattern is one application route's method and net/http.ServeMux
// path pattern — nothing about its handler, request schema, or
// authorization requirements (Milestone 8 Part 5 deliberately keeps this
// narrow; see its own tests for why). net/http.ServeMux has no public way
// to enumerate every pattern it holds once registered, so this is
// collected once, here, at registration time, rather than hand-duplicated
// in a second list a test would have to keep in sync by hand.
type RoutePattern struct {
	Method string
	Path   string
}

// RoutePatterns returns the method+path of every route this application
// registers — /health, /health/db and /api/v1/openapi.yaml included —
// populated by the most recent call to Handler(). Route registration is
// entirely static (there is no dynamic/conditional registration anywhere
// in this package), so calling Handler() once is enough for this to
// reflect the complete, real route table for the lifetime of the App.
func (a *App) RoutePatterns() []RoutePattern {
	return a.routes
}

// Handler builds the complete application handler: every registered
// route, wrapped first by WrapMethodNotAllowed (so a wrong-method request
// against any registered path gets the standard JSON error envelope
// instead of net/http's plain-text default) and then by Recover (so a
// panic anywhere below never reaches the client as a dropped connection).
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

	// register is the single place a route's method+path ever gets
	// written down: it both wires handlerFunc into mux under net/http's
	// "METHOD /path" pattern syntax and records the same (method, path)
	// pair into a.routes, so RoutePatterns() can never drift from what
	// was actually registered — see RoutePattern's own doc comment.
	a.routes = nil
	register := func(method, path string, handlerFunc http.HandlerFunc) {
		mux.HandleFunc(method+" "+path, handlerFunc)
		a.routes = append(a.routes, RoutePattern{Method: method, Path: path})
	}

	// Wire the organisation dependency chain:
	// pool -> repository -> service -> handler. OrganisationService also
	// depends on the settings repository to provision a default settings
	// row for every new organisation (see OrganisationService.Create).
	organisationRepository := admin.NewPostgresOrganisationRepository(a.db)
	settingsRepository := admin.NewPostgresSettingsRepository(a.db)
	organisationService := admin.NewOrganisationService(organisationRepository, settingsRepository)
	organisationHandler := admin.NewOrganisationHandler(organisationService)

	// Wire the user dependency chain: pool -> repository -> service ->
	// handler. UserService also depends on the organisation repository
	// (already constructed above) to check that the requesting
	// organisation exists before a user is created against it.
	userRepository := admin.NewPostgresUserRepository(a.db)
	userService := admin.NewUserService(userRepository, organisationRepository)
	userHandler := admin.NewUserHandler(userService)

	// Wire the auth dependency chain: pool -> repository -> service ->
	// handler/middleware. AuthService also depends on the user repository
	// (already constructed above) to look users up by email, and on the
	// pool itself (a.db satisfies auth's TxBeginner directly) to commit a
	// new session and the user's LastLogin update as a single
	// transaction. AuthMiddleware depends on both the session and user
	// repositories to validate a bearer token on every protected route.
	sessionRepository := admin.NewPostgresSessionRepository(a.db)
	authService := admin.NewAuthService(userRepository, sessionRepository, a.db)
	authHandler := admin.NewAuthHandler(authService)
	authMiddleware := admin.NewAuthMiddleware(sessionRepository, userRepository)

	// Milestone 13 Part 4: login and registration are the only public
	// mutating routes, so they're limited per client address (see
	// defaultRateLimits and httpx.ClientAddressKey) — before the body is
	// even decoded, and identically for right and wrong credentials, so a
	// 429 reveals nothing about any account.
	loginLimit := httpx.RateLimit(metrics.RateLimiterLogin, a.rateLimits.login.newLimiter(), httpx.ClientAddressKey, a.metrics)
	registerLimit := httpx.RateLimit(metrics.RateLimiterRegister, a.rateLimits.register.newLimiter(), httpx.ClientAddressKey, a.metrics)

	register("POST", apiV1Prefix+"/auth/login", loginLimit(authHandler.Login))
	// POST /auth/logout (Milestone 8 Part 3): revokes only the current
	// session (see AuthHandler.Logout) — authentication required, so it
	// must go through the same RequireAuth every other protected route
	// does, not a bespoke check.
	register("POST", apiV1Prefix+"/auth/logout", authMiddleware.RequireAuth(authHandler.Logout))

	// Wire the registration dependency chain: pool -> repositories ->
	// service -> handler. POST /register (Milestone 4 Part 5) is now the
	// ONLY way an organisation and its first (admin) user can ever be
	// created — it replaces the old public POST /organisations and public
	// POST /users?organisationId= bootstrap routes entirely; neither is
	// registered any more. RegistrationService owns its own transaction
	// (a.db satisfies its TxBeginner directly) across the organisation,
	// settings, and first-user inserts.
	registrationService := admin.NewRegistrationService(organisationRepository, settingsRepository, userRepository, a.db)
	registrationHandler := admin.NewRegistrationHandler(registrationService)

	register("POST", apiV1Prefix+"/register", registerLimit(registrationHandler.Register))

	// POST /users (Milestone 4 Part 5) is now a protected, role-gated
	// route for creating additional users within an existing,
	// already-bootstrapped organisation: only admin or manager may reach
	// it at all (RequireRole), organisation identity comes exclusively
	// from AuthenticatedUser.OrganisationID (no organisationId query
	// parameter is read anywhere any more), and UserService.Create
	// itself enforces which target role the caller's role may assign —
	// see that method's doc comment for why that rule lives there too,
	// not only in this route gate.
	register(
		"POST", apiV1Prefix+"/users",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin, admin.UserRoleManager)(userHandler.Create)),
	)
	// GET /users (Milestone 8 Part 3): organisation user/team management —
	// admin/manager only, the same gate POST /users already uses. An
	// ordinary user must get 403, never a partial/self-only listing.
	register(
		"GET", apiV1Prefix+"/users",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin, admin.UserRoleManager)(userHandler.List)),
	)
	register("GET", apiV1Prefix+"/users/{id}", authMiddleware.RequireAuth(userHandler.GetByID))

	// GET /organisation (Milestone 4 Part 4) is a self-resource route: it
	// always returns the authenticated caller's own organisation, sourced
	// from AuthenticatedUser.OrganisationID — there is no {id} path
	// segment, so there is no client-supplied organisation identifier to
	// remove or ignore here. This replaces the earlier protected
	// GET /organisations/{id}.
	register("GET", apiV1Prefix+"/organisation", authMiddleware.RequireAuth(organisationHandler.GetCurrent))

	// PATCH /organisation (Milestone 7 Part 1) is admin-only: organisation
	// identity/legal/business details affect every invoice the tenant
	// produces, so it isn't freely mutable by every role the way customer/
	// product/invoice data is. Same RequireRole gate POST /users already
	// uses, no new role-hierarchy logic.
	register(
		"PATCH", apiV1Prefix+"/organisation",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin)(organisationHandler.Update)),
	)

	// Wire the settings dependency chain: pool -> repository -> service ->
	// handler. settingsRepository is already constructed above (used for
	// invoice numbering) — Settings' own service/handler are new
	// (Milestone 8 Part 3).
	settingsHandler := admin.NewSettingsHandler(admin.NewSettingsService(settingsRepository))

	// GET /organisation/settings: every authenticated role may read the
	// organisation's current invoice-numbering currency/prefix/terms.
	register("GET", apiV1Prefix+"/organisation/settings", authMiddleware.RequireAuth(settingsHandler.GetCurrent))
	// PATCH /organisation/settings: admin-only, same gate as PATCH
	// /organisation — these settings affect every future invoice.
	register(
		"PATCH", apiV1Prefix+"/organisation/settings",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin)(settingsHandler.Update)),
	)

	// Wire the customer dependency chain: pool -> repository -> service -> handler.
	// AddressRepository (Milestone 7 Part 1) is a second, small dependency
	// of CustomerService, for a customer's billing address — see
	// CustomerService's own doc comment for why it lives there rather than
	// a separate service.
	customerRepository := customer.NewPostgresCustomerRepository(a.db)
	addressRepository := customer.NewPostgresAddressRepository(a.db)
	customerService := customer.NewCustomerService(customerRepository, addressRepository)
	customerHandler := customer.NewCustomerHandler(customerService)

	register("POST", apiV1Prefix+"/customers", authMiddleware.RequireAuth(customerHandler.Create))
	// GET /customers (Milestone 8 Part 3): open to every authenticated
	// role, same policy as every other customer route.
	register("GET", apiV1Prefix+"/customers", authMiddleware.RequireAuth(customerHandler.List))
	register("GET", apiV1Prefix+"/customers/{id}", authMiddleware.RequireAuth(customerHandler.GetByID))

	// Billing address (Milestone 7 Part 1): available to every authenticated
	// role, same policy as every other customer/invoice business-data route
	// — only organisation PATCH is admin-only.
	register("GET", apiV1Prefix+"/customers/{id}/billing-address", authMiddleware.RequireAuth(customerHandler.GetBillingAddress))
	register("PUT", apiV1Prefix+"/customers/{id}/billing-address", authMiddleware.RequireAuth(customerHandler.UpsertBillingAddress))

	// Wire the product dependency chain: pool -> repository -> service -> handler.
	productRepository := product.NewPostgresProductRepository(a.db)
	productService := product.NewProductService(productRepository)
	productHandler := product.NewProductHandler(productService)

	register("POST", apiV1Prefix+"/products", authMiddleware.RequireAuth(productHandler.Create))
	// GET /products (Milestone 8 Part 3): open to every authenticated role.
	register("GET", apiV1Prefix+"/products", authMiddleware.RequireAuth(productHandler.List))
	register("GET", apiV1Prefix+"/products/{id}", authMiddleware.RequireAuth(productHandler.GetByID))

	// Wire the invoice dependency chain: pool -> repository -> service -> handler.
	// The invoice service also depends on the customer and product
	// repositories (already constructed above) to check, within the
	// requesting organisation, that a referenced customer/product exists,
	// on the settings repository (also constructed above) to allocate
	// each invoice's sequential number, on the payment repository for
	// CreatePayment, and on the pool itself to begin the transaction that
	// makes invoice/payment creation atomic (a.db satisfies
	// invoice.TxBeginner directly). organisationRepository and
	// addressRepository (both already constructed above, for the
	// organisation/customer routes) are Milestone 7 Part 2's additions:
	// Send uses them, within its own transaction, to capture the
	// invoice's immutable seller/customer-billing-address snapshot.
	invoiceRepository := invoice.NewPostgresInvoiceRepository(a.db)
	paymentRepository := invoice.NewPostgresPaymentRepository(a.db)
	invoiceService := invoice.NewInvoiceService(invoiceRepository, customerRepository, productRepository, organisationRepository, addressRepository, settingsRepository, paymentRepository, a.db)

	// InvoicePDFService (Milestone 7 Part 3) reuses the exact same
	// tenant-scoped repositories as InvoiceService — no PDF-specific
	// repository or query exists. InvoicePDFRenderer is stateless (only
	// holds the embedded font bytes) and safe to share across requests.
	invoicePDFRenderer := invoice.NewInvoicePDFRenderer()
	invoicePDFService := invoice.NewInvoicePDFService(invoiceRepository, paymentRepository, organisationRepository, customerRepository, addressRepository, settingsRepository, invoicePDFRenderer, a.metrics)

	invoiceHandler := invoice.NewInvoiceHandler(invoiceService, invoicePDFService, a.metrics)

	register("POST", apiV1Prefix+"/invoices", authMiddleware.RequireAuth(invoiceHandler.Create))
	// GET /invoices (Milestone 8 Part 3): open to every authenticated
	// role, same policy as every other invoice route.
	register("GET", apiV1Prefix+"/invoices", authMiddleware.RequireAuth(invoiceHandler.List))
	register("GET", apiV1Prefix+"/invoices/{id}", authMiddleware.RequireAuth(invoiceHandler.GetByID))
	// POST /invoices/{id}/send (Milestone 5) is a lifecycle finalisation
	// operation only — no PDF, no email — available to every authenticated
	// role, same as every other invoice/payment route.
	register("POST", apiV1Prefix+"/invoices/{id}/send", authMiddleware.RequireAuth(invoiceHandler.Send))
	register("POST", apiV1Prefix+"/invoices/{id}/payments", authMiddleware.RequireAuth(invoiceHandler.CreatePayment))
	register("GET", apiV1Prefix+"/invoices/{id}/payments", authMiddleware.RequireAuth(invoiceHandler.GetPayments))
	// GET /invoices/{id}/pdf (Milestone 7 Part 3): synchronous PDF
	// generation, same open-to-all-authenticated-roles policy as every
	// other invoice route.
	//
	// Rate-limited per authenticated user (Milestone 13 Part 4): PDF
	// rendering is synchronous and CPU-bound. The limiter sits inside
	// RequireAuth, so an unauthenticated request is a 401 and never
	// consumes a token, and a user's identity — not their address — is
	// the key.
	pdfLimit := httpx.RateLimit(metrics.RateLimiterPDF, a.rateLimits.pdf.newLimiter(), authenticatedUserKey, a.metrics)
	register("GET", apiV1Prefix+"/invoices/{id}/pdf", authMiddleware.RequireAuth(pdfLimit(invoiceHandler.GetPDF)))

	// /health and /health/db (Milestone 1) stay unversioned and require
	// no authentication — they are infrastructure probes, not part of the
	// versioned application contract. Both use method-specific patterns
	// so a wrong-method request is handled by ServeMux's own routing
	// (converted to the JSON envelope by WrapMethodNotAllowed below, the
	// same as every other route) rather than a second, bespoke method
	// check duplicating that logic here.
	register("GET", "/health", func(w http.ResponseWriter, r *http.Request) {
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	register("GET", "/health/db", func(w http.ResponseWriter, r *http.Request) {
		if err := a.db.Ping(r.Context()); err != nil {
			httpx.WriteJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "unavailable"})
			return
		}

		httpx.WriteJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	// GET /api/v1/openapi.yaml (Milestone 8 Part 4): serves api/openapi.go's
	// embedded copy of api/openapi.yaml verbatim — the same maintained file
	// this project's API contract is documented in, never a second,
	// separately generated document. Deliberately public: a client needs
	// this before it can know how to authenticate at all.
	register("GET", apiV1Prefix+"/openapi.yaml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/yaml")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(openapi.Spec)
	})

	// GET /metrics (Milestone 10 Part 4): the Prometheus scrape endpoint.
	// Deliberately registered directly on mux, bypassing the register()
	// helper every other route above uses — it is operational telemetry,
	// not part of the versioned business API, so it must never appear in
	// a.routes (RoutePatterns()), which TestRoutes_MatchOpenAPISpec
	// compares against api/openapi.yaml. Documenting an operational
	// endpoint in the public API contract would be the actual drift that
	// test exists to catch, not this one's absence from it.
	//
	// Registered only when metrics are enabled (a.metrics != nil — see
	// config.Config.MetricsEnabled and New's own doc comment): section
	// 32's preferred disabled-state behaviour is an absent route (a plain
	// ServeMux 404, exactly like any other unregistered path) rather than
	// a bespoke "metrics disabled" JSON body.
	//
	// It still passes through every middleware layer below (RequestID,
	// RequestLogging, Recover, WrapMethodNotAllowed) for the same request-
	// ID/panic-safety every other route gets — RequestLogging just
	// deliberately excludes this exact path from both the business HTTP
	// metrics it records (see isMetricsRequest in httpx/middleware.go:
	// counting a metrics scrape as an HTTP request would be pointless,
	// ever-growing self-referential traffic) and the ordinary per-request
	// INFO log on success (same treatment as /health).
	if a.metrics != nil {
		mux.Handle("GET /metrics", a.metrics.Handler())
	}

	// Section 9 (Milestone 8 Part 2): router-generated 404 is
	// deliberately NOT brought into the JSON envelope, unlike 405 below.
	// A catch-all "/" mux pattern was tried and rejected: net/http.
	// ServeMux treats any registered pattern rooted at "/" as a path
	// match for every otherwise-unregistered path, which corrupts its
	// own 405-vs-404 distinction application-wide — verified empirically
	// against this exact test suite. Since correct 405/Allow-header
	// behaviour matters more than a nicer body on a genuinely-unmatched
	// path, an unmatched route is left to ServeMux's stdlib default
	// ("404 page not found", plain text) for every case except one:
	// FrontendFallback below, which reuses that same r.Pattern signal to
	// let the embedded frontend (internal/webui) render its SPA shell —
	// or a real static asset — for a GET/HEAD request specifically,
	// without ever registering anything on mux itself. See
	// httpx.FrontendFallback's own doc comment for the full reasoning,
	// including why it cannot reintroduce the "/" catch-all problem this
	// paragraph describes.
	return httpx.RequestID(
		httpx.RequestLogging(a.logger, a.metrics)(
			httpx.Recover(a.logger)(
				httpx.FrontendFallback(webui.Serve)(
					httpx.WrapMethodNotAllowed(mux),
				),
			),
		),
	)
}

// authenticatedUserKey keys a request by its authenticated user. Only
// used behind RequireAuth, which guarantees an identity is present.
func authenticatedUserKey(r *http.Request) string {
	identity, _ := admin.AuthenticatedUserFromContext(r.Context())
	return "user:" + identity.UserID.String()
}
