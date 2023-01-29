package models

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type Talkgroup struct {
	ID          uint           `json:"id" gorm:"primaryKey"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Admins      []User         `json:"admins" gorm:"many2many:talkgroup_admins;"`
	NCOs        []User         `json:"ncos" gorm:"many2many:talkgroup_ncos;"`
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
	db.Preload("Admins").Preload("NCOs").First(&talkgroup, ID)
	return talkgroup
}

func DeleteTalkgroup(db *gorm.DB, id uint) {
	db.Unscoped().Select(clause.Associations, "Admins").Select(clause.Associations, "NCOs").Delete(&Talkgroup{ID: id})
}
