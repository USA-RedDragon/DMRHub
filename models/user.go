package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint   `gorm:"primaryKey"`
	Callsign  string `gorm:"uniqueIndex"`
	Username  string `gorm:"uniqueIndex"`
	Password  string
	Admin     bool
	Repeaters []Repeater
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}
