package product

import (
	"time"

	"gorm.io/gorm"
)

type Product struct {
	ID             string `gorm:"primaryKey"`
	OrganisationID string `gorm:"index"`
	Name           string `gorm:"index"`
	Description    string `gorm:"type:text"`
	SKU            string `gorm:"uniqueIndex:idx_product_org"`
	Price          float64
	Category       string
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

func (p *Product) TableName() string {
	return "products"
}

func (p *Product) IsAvailable() bool {
	return p.IsActive
}
