package admin

import (
	"time"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

type Settings struct {
	ID             uuid.UUID `gorm:"primaryKey"`
	OrganisationID uuid.UUID `gorm:"uniqueIndex"`
	InvoicePrefix  string    // INV-, etc.
	InvoiceNumber  int       // Next invoice number
	Currency       string
	PaymentTerms   int // Days
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

func (s *Settings) TableName() string {
	return "settings"
}

func (s *Settings) NextInvoiceNumber() int {
	s.InvoiceNumber++
	return s.InvoiceNumber
}
