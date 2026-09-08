package invoice

import (
	"time"

	"gorm.io/gorm"
)

type Invoice struct {
	ID             string `gorm:"primaryKey"`
	OrganisationID string `gorm:"index"`
	CustomerID     string `gorm:"index"`
	InvoiceNumber  string `gorm:"uniqueIndex:idx_invoice_org"`
	IssueDate      time.Time
	DueDate        time.Time
	Subtotal       int64
	VatTotal       int64
	Total          int64
	Status         string // draft, sent, paid, overdue, cancelled
	Notes          string `gorm:"type:text"`
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`

	Lines    []Line    `gorm:"foreignKey:InvoiceID"`
	Payments []Payment `gorm:"foreignKey:InvoiceID"`
}

func (i *Invoice) TableName() string {
	return "invoices"
}

func (i *Invoice) IsOverdue() bool {
	return i.Status != "paid" && time.Now().After(i.DueDate)
}

func (i *Invoice) RemainingBalance() int64 {
	paid := int64(0)
	for _, p := range i.Payments {
		paid += p.Amount
	}
	return i.Total - paid
}
