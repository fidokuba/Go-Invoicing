package admin

import (
	"time"

	"gorm.io/gorm"
)

type Organisation struct {
	ID         uint   `gorm:"primaryKey"`
	Name       string `gorm:"index"`
	Email      string
	Phone      string
	Website    string
	Logo       string
	Address    string
	City       string
	State      string
	PostalCode string
	Country    string
	TaxID      string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	DeletedAt  gorm.DeletedAt `gorm:"index"`
}

func (o *Organisation) TableName() string {
	return "organisations"
}
