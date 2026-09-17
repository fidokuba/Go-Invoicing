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

	mux.HandleFunc("POST /organisations", organisationHandler.Create)
	mux.HandleFunc("GET /organisations/{id}", organisationHandler.GetByID)

	// Wire the customer dependency chain: pool -> repository -> service -> handler.
	customerRepository := customer.NewPostgresCustomerRepository(a.db)
	customerService := customer.NewCustomerService(customerRepository)
	customerHandler := customer.NewCustomerHandler(customerService)

	mux.HandleFunc("POST /customers", customerHandler.Create)
	mux.HandleFunc("GET /customers/{id}", customerHandler.GetByID)

	// Wire the product dependency chain: pool -> repository -> service -> handler.
	productRepository := product.NewPostgresProductRepository(a.db)
	productService := product.NewProductService(productRepository)
	productHandler := product.NewProductHandler(productService)

	mux.HandleFunc("POST /products", productHandler.Create)
	mux.HandleFunc("GET /products/{id}", productHandler.GetByID)

	// Wire the invoice dependency chain: pool -> repository -> service -> handler.
	// The invoice service also depends on the customer and product
	// repositories (already constructed above) to check, within the
	// requesting organisation, that a referenced customer/product exists,
	// on the settings repository (also constructed above) to allocate
	// each invoice's sequential number, and on the pool itself to begin
	// the transaction that makes invoice creation atomic (a.db satisfies
	// invoice.TxBeginner directly).
	invoiceRepository := invoice.NewPostgresInvoiceRepository(a.db)
	invoiceService := invoice.NewInvoiceService(invoiceRepository, customerRepository, productRepository, settingsRepository, a.db)
	invoiceHandler := invoice.NewInvoiceHandler(invoiceService)

	mux.HandleFunc("POST /invoices", invoiceHandler.Create)
	mux.HandleFunc("GET /invoices/{id}", invoiceHandler.GetByID)

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
