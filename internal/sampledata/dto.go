package sampledata

// CreateTestDataResponse is what POST /api/v1/test-data returns: a count
// of what it actually created, not the records themselves — the caller
// already has GET /customers, /products, /invoices to look at those.
type CreateTestDataResponse struct {
	CustomersCreated int `json:"customersCreated"`
	ProductsCreated  int `json:"productsCreated"`
	InvoicesCreated  int `json:"invoicesCreated"`
	InvoicesSent     int `json:"invoicesSent"`
	InvoicesPaid     int `json:"invoicesPaid"`
}

func newCreateTestDataResponse(s Summary) CreateTestDataResponse {
	return CreateTestDataResponse{
		CustomersCreated: s.CustomersCreated,
		ProductsCreated:  s.ProductsCreated,
		InvoicesCreated:  s.InvoicesCreated,
		InvoicesSent:     s.InvoicesSent,
		InvoicesPaid:     s.InvoicesPaid,
	}
}
