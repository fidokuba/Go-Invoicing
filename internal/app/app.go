package app

import (
	"encoding/json"
	"net/http"

	admin "go-invoicing/internal/administration"
	"go-invoicing/internal/customer"
	"go-invoicing/internal/invoice"
	"go-invoicing/internal/product"

	"github.com/jackc/pgx/v5/pgxpool"
)

type App struct {
	db *pgxpool.Pool
}

// initialises a database connection
func New(db *pgxpool.Pool) *App {
	return &App{db: db}
}

// creates a /health edpoint to check if the server connection is ok
func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()

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

	mux.HandleFunc("POST /auth/login", authHandler.Login)

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

	mux.HandleFunc("POST /register", registrationHandler.Register)

	// POST /users (Milestone 4 Part 5) is now a protected, role-gated
	// route for creating additional users within an existing,
	// already-bootstrapped organisation: only admin or manager may reach
	// it at all (RequireRole), organisation identity comes exclusively
	// from AuthenticatedUser.OrganisationID (no organisationId query
	// parameter is read anywhere any more), and UserService.Create
	// itself enforces which target role the caller's role may assign —
	// see that method's doc comment for why that rule lives there too,
	// not only in this route gate.
	mux.HandleFunc(
		"POST /users",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin, admin.UserRoleManager)(userHandler.Create)),
	)
	mux.HandleFunc("GET /users/{id}", authMiddleware.RequireAuth(userHandler.GetByID))

	// GET /organisation (Milestone 4 Part 4) is a self-resource route: it
	// always returns the authenticated caller's own organisation, sourced
	// from AuthenticatedUser.OrganisationID — there is no {id} path
	// segment, so there is no client-supplied organisation identifier to
	// remove or ignore here. This replaces the earlier protected
	// GET /organisations/{id}.
	mux.HandleFunc("GET /organisation", authMiddleware.RequireAuth(organisationHandler.GetCurrent))

	// PATCH /organisation (Milestone 7 Part 1) is admin-only: organisation
	// identity/legal/business details affect every invoice the tenant
	// produces, so it isn't freely mutable by every role the way customer/
	// product/invoice data is. Same RequireRole gate POST /users already
	// uses, no new role-hierarchy logic.
	mux.HandleFunc(
		"PATCH /organisation",
		authMiddleware.RequireAuth(admin.RequireRole(admin.UserRoleAdmin)(organisationHandler.Update)),
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

	mux.HandleFunc("POST /customers", authMiddleware.RequireAuth(customerHandler.Create))
	mux.HandleFunc("GET /customers/{id}", authMiddleware.RequireAuth(customerHandler.GetByID))

	// Billing address (Milestone 7 Part 1): available to every authenticated
	// role, same policy as every other customer/invoice business-data route
	// — only organisation PATCH is admin-only.
	mux.HandleFunc("GET /customers/{id}/billing-address", authMiddleware.RequireAuth(customerHandler.GetBillingAddress))
	mux.HandleFunc("PUT /customers/{id}/billing-address", authMiddleware.RequireAuth(customerHandler.UpsertBillingAddress))

	// Wire the product dependency chain: pool -> repository -> service -> handler.
	productRepository := product.NewPostgresProductRepository(a.db)
	productService := product.NewProductService(productRepository)
	productHandler := product.NewProductHandler(productService)

	mux.HandleFunc("POST /products", authMiddleware.RequireAuth(productHandler.Create))
	mux.HandleFunc("GET /products/{id}", authMiddleware.RequireAuth(productHandler.GetByID))

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
	invoiceHandler := invoice.NewInvoiceHandler(invoiceService)

	mux.HandleFunc("POST /invoices", authMiddleware.RequireAuth(invoiceHandler.Create))
	mux.HandleFunc("GET /invoices/{id}", authMiddleware.RequireAuth(invoiceHandler.GetByID))
	// POST /invoices/{id}/send (Milestone 5) is a lifecycle finalisation
	// operation only — no PDF, no email — available to every authenticated
	// role, same as every other invoice/payment route.
	mux.HandleFunc("POST /invoices/{id}/send", authMiddleware.RequireAuth(invoiceHandler.Send))
	mux.HandleFunc("POST /invoices/{id}/payments", authMiddleware.RequireAuth(invoiceHandler.CreatePayment))
	mux.HandleFunc("GET /invoices/{id}/payments", authMiddleware.RequireAuth(invoiceHandler.GetPayments))

	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{
			"status": "ok",
		})
	})

	// creates a /health/db endpoint to check if the database conection is ok
	mux.HandleFunc("/health/db", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			writeJSON(w, http.StatusMethodNotAllowed, map[string]string{
				"error": "method not allowed",
			})
			return
		}

		if err := a.db.Ping(r.Context()); err != nil {
			writeJSON(w, http.StatusServiceUnavailable, map[string]string{
				"status": "unavailable",
			})
			return
		}

		writeJSON(w, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})

	return mux

}

// creates JSON string to be sent back as a response to the calling client
func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
