package models

import (
	"time"

	"gorm.io/gorm"
)

type Talkgroup struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Admins      []User         `json:"admins" gorm:"many2many:talkgroup_admins;"`
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"-"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}

func TalkgroupIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&Talkgroup{}).Where("ID = ?", id).Limit(1).Count(&count)
	return count > 0
}

func FindTalkgroupByID(db *gorm.DB, ID uint) Talkgroup {
	var talkgroup Talkgroup
	db.Preload("Admins").First(&talkgroup, ID)
	return talkgroup
}
