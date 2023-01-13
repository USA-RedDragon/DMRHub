package models

import (
	"time"

	"gorm.io/gorm"
)

type Talkgroup struct {
	ID            uint `gorm:"primaryKey"`
	Name          string
	TS1Talkgroups []Talkgroup
	TS2Talkgroups []Talkgroup
	CreatedAt     time.Time      `json:"-"`
	UpdatedAt     time.Time      `json:"-"`
	DeletedAt     gorm.DeletedAt `gorm:"index" json:"-"`
}
