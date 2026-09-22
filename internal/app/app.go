package app

import (
	"log/slog"
	"net/http"

	"go-invoicing/api"
	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/httpx"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"

	"github.com/jackc/pgx/v5/pgxpool"
)

// apiV1Prefix is prepended to every versioned application route
// (Milestone 8 Part 2). /health and /health/db are deliberately excluded
// — they are infrastructure-level probes, not part of the versioned
// application contract, and must keep working regardless of API version.
const apiV1Prefix = "/api/v1"

type App struct {
	db     *pgxpool.Pool
	logger *slog.Logger

	// routes is populated by Handler() as it registers each route — see
	// RoutePattern's own doc comment for why this exists and how tests
	// use it.
	routes []RoutePattern
}

// New wires an App around an existing database pool and logger. logger is
// used only by the panic-recovery middleware (see Handler) to record an
// unexpected handler panic server-side before responding with the
// standard generic 500 — nothing else in this package logs anything.
//
// db may be a typed nil *pgxpool.Pool: every repository constructor
// Handler calls below only stores it in a struct field, never dereferences
// it during construction, so building the route table (see RoutePatterns)
// requires no live database connection. A nil pool only becomes a problem
// once a handler that actually queries the database is invoked.
func New(db *pgxpool.Pool, logger *slog.Logger) *App {
	return &App{db: db, logger: logger}
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

	register("POST", apiV1Prefix+"/auth/login", authHandler.Login)
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

	register("POST", apiV1Prefix+"/register", registrationHandler.Register)

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
	invoicePDFService := invoice.NewInvoicePDFService(invoiceRepository, paymentRepository, organisationRepository, customerRepository, addressRepository, settingsRepository, invoicePDFRenderer)

	invoiceHandler := invoice.NewInvoiceHandler(invoiceService, invoicePDFService)

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
	register("GET", apiV1Prefix+"/invoices/{id}/pdf", authMiddleware.RequireAuth(invoiceHandler.GetPDF))

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

	// Section 9 (Milestone 8 Part 2): router-generated 404 is
	// deliberately NOT brought into the JSON envelope, unlike 405 below.
	// A catch-all "/" pattern was tried and rejected: net/http.ServeMux
	// treats any registered pattern — including a maximally general "/"
	// — as a valid handler for a method that pattern accepts, which is
	// every method for an unrestricted "/". That makes ServeMux prefer
	// falling through to "/" over synthesizing its own 405 for a
	// wrong-method request against any OTHER, more specific route in
	// this file — verified empirically: registering a "/" catch-all
	// silently turned every wrong-method request mux-wide from a 405
	// into a 404. Since correct 405/Allow-header behaviour matters more
	// than a JSON body on a genuinely-unmatched path, an unmatched route
	// is left to ServeMux's stdlib default ("404 page not found",
	// plain text) — the documented exception this section's own
	// instructions anticipate ("if doing so would require ... fighting
	// ServeMux semantics, leave router-generated 404 ... alone").
	return httpx.RequestID(
		httpx.RequestLogging(a.logger)(
			httpx.Recover(a.logger)(
				httpx.WrapMethodNotAllowed(mux),
			),
		),
	)
}
