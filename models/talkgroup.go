package models

import (
	"time"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/klog/v2"
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

func FindTalkgroupsByOwnerID(db *gorm.DB, ownerID uint) ([]Talkgroup, error) {
	var talkgroups []Talkgroup
	if err := db.Joins("JOIN talkgroup_admins on talkgroup_admins.talkgroup_id=talkgroups.id").
		Joins("JOIN users on talkgroup_admins.user_id=users.id").Order("id asc").Where("users.id=?", ownerID).
		Group("talkgroups.id").Find(&talkgroups).Error; err != nil {
		klog.Errorf("Error getting talkgroups owned by user %d: %v", ownerID, err)
		return nil, err
	}
	return talkgroups, nil
}
