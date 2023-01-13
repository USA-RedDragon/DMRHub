package models

import (
	"time"

	"gorm.io/gorm"
)

type Talkgroup struct {
	ID          uint `gorm:"primaryKey"`
	Name        string
	Description string
	Admins      []User         `gorm:"many2many:talkgroup_admins;"`
	CreatedAt   time.Time      `json:"-"`
	UpdatedAt   time.Time      `json:"-"`
	DeletedAt   gorm.DeletedAt `gorm:"index" json:"-"`
}
