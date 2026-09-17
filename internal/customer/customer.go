package customer

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Customer struct {
	ID             uuid.UUID `gorm:"primaryKey"`
	OrganisationID uuid.UUID `gorm:"index"`
	Name           string    `gorm:"index"`
	Email          string
	Phone          string
	CompanyName    string
	TaxID          string
	Status         string // active, inactive, archived
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`

	Addresses []Address `gorm:"foreignKey:CustomerID"`
}

func (c *Customer) TableName() string {
	return "customers"
}

func (c *Customer) IsActive() bool {
	return c.Status == "active"
}
