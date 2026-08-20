package admin

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID             uint           `gorm:"primaryKey"`
	OrganisationID uint           `gorm:"index"`
	Name           string
	Email          string `gorm:"uniqueIndex:idx_user_org"`
	PasswordHash   string
	Role           string // admin, manager, user
	IsActive       bool
	LastLogin      *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
	DeletedAt      gorm.DeletedAt `gorm:"index"`
}

func (u *User) TableName() string {
	return "users"
}

func (u *User) IsAdmin() bool {
	return u.Role == "admin"
}
