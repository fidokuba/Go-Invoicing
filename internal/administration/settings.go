package admin

import (
	"time"

	"gorm.io/gorm"
)

type Settings struct {
	ID             string `gorm:"primaryKey"`
	OrganisationID string `gorm:"uniqueIndex"`
	InvoicePrefix  string // INV-, etc.
	InvoiceNumber  int    // Next invoice number
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
