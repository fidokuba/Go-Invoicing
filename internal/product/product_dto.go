package product

import "time"

// CreateProductRequest is the shape a client may POST to create a product.
// It deliberately exposes only what a caller is allowed to set — not ID,
// OrganisationID, IsActive, CreatedAt, DeletedAt, or anything else the
// server owns.
//
// Price is an integer minor-unit value (e.g. cents), matching the
// database column and the convention used elsewhere for money — never a
// float.
type CreateProductRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	SKU         string `json:"sku"`
	Price       int64  `json:"price"`
	Category    string `json:"category"`
}

// ProductResponse is the shape returned to clients. It exposes the public
// product fields but never DeletedAt.
type ProductResponse struct {
	ID             string  `json:"id"`
	OrganisationID string  `json:"organisationId"`
	Name           string  `json:"name"`
	Description    *string `json:"description,omitempty"`
	SKU            string  `json:"sku"`
	Price          int64   `json:"price"`
	Category       *string `json:"category,omitempty"`
	IsActive       bool    `json:"isActive"`
	CreatedAt      string  `json:"createdAt"`
	UpdatedAt      string  `json:"updatedAt"`
}

// toProductResponse maps the internal domain model onto the API's response
// shape.
func toProductResponse(p *Product) ProductResponse {
	return ProductResponse{
		ID:             p.ID.String(),
		OrganisationID: p.OrganisationID.String(),
		Name:           p.Name,
		Description:    p.Description,
		SKU:            p.SKU,
		Price:          p.Price,
		Category:       p.Category,
		IsActive:       p.IsActive,
		CreatedAt:      p.CreatedAt.Format(time.RFC3339),
		UpdatedAt:      p.UpdatedAt.Format(time.RFC3339),
	}
}
