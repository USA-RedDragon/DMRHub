package models

import (
	"time"

	gorm_seeder "github.com/kachit/gorm-seeder"
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

func ListTalkgroups(db *gorm.DB) []Talkgroup {
	var talkgroups []Talkgroup
	db.Preload("Admins").Preload("NCOs").Order("id asc").Find(&talkgroups)
	return talkgroups
}

func CountTalkgroups(db *gorm.DB) int {
	var count int64
	db.Model(&Talkgroup{}).Count(&count)
	return int(count)
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

func CountTalkgroupsByOwnerID(db *gorm.DB, ownerID uint) int {
	var count int64
	db.Model(&Talkgroup{}).Joins("JOIN talkgroup_admins on talkgroup_admins.talkgroup_id=talkgroups.id").
		Joins("JOIN users on talkgroup_admins.user_id=users.id").
		Where("users.id=?", ownerID).Count(&count)
	return int(count)
}

type TalkgroupsSeeder struct {
	gorm_seeder.SeederAbstract
}

func NewTalkgroupsSeeder(cfg gorm_seeder.SeederConfiguration) TalkgroupsSeeder {
	return TalkgroupsSeeder{gorm_seeder.NewSeederAbstract(cfg)}
}

func (s *TalkgroupsSeeder) Seed(db *gorm.DB) error {
	var talkgroups = []Talkgroup{
		{
			ID:          uint(9990),
			Name:        "DMRHub Parrot",
			Description: "This talkgroup will not be routed to any repeaters and Parrot will respond with a private call.",
		},
	}
	return db.CreateInBatches(talkgroups, s.Configuration.Rows).Error
}

func (s *TalkgroupsSeeder) Clear(db *gorm.DB) error {
	return nil
}
