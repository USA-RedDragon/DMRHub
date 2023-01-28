package models

import (
	"time"

	"gorm.io/gorm"
)

type AppSettings struct {
	ID        uint `gorm:"primaryKey"`
	HasSeeded bool
	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}
