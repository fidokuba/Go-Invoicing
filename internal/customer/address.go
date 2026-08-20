package customer

import (
	"time"

	"gorm.io/gorm"
)

type Address struct {
	ID        uint           `gorm:"primaryKey"`
	CustomerID uint           `gorm:"index"`
	Type      string         // billing, shipping, other
	Street    string
	City      string
	State     string
	PostalCode string
	Country   string
	IsDefault bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func (a *Address) TableName() string {
	return "addresses"
}
