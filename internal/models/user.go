package models

import (
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	gorm_seeder "github.com/kachit/gorm-seeder"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"k8s.io/klog/v2"
)

type User struct {
	ID        uint           `json:"id" gorm:"primaryKey" binding:"required"`
	Callsign  string         `json:"callsign" gorm:"uniqueIndex" binding:"required"`
	Username  string         `json:"username" gorm:"uniqueIndex" binding:"required"`
	Password  string         `json:"-"`
	Admin     bool           `json:"admin"`
	Approved  bool           `json:"approved" binding:"required"`
	Suspended bool           `json:"suspended"`
	Repeaters []Repeater     `json:"repeaters" gorm:"foreignKey:OwnerID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}

func (u User) TableName() string {
	return "users"
}

func UserExists(db *gorm.DB, user User) bool {
	var count int64
	db.Model(&User{}).Where("ID = ?", user.ID).Limit(1).Count(&count)
	return count > 0
}

func UserIDExists(db *gorm.DB, id uint) bool {
	var count int64
	db.Model(&User{}).Where("ID = ?", id).Limit(1).Count(&count)
	return count > 0
}

func FindUserByID(db *gorm.DB, ID uint) User {
	var user User
	db.Preload("Repeaters").First(&user, ID)
	return user
}

func ListUsers(db *gorm.DB) []User {
	var users []User
	db.Preload("Repeaters").Find(&users)
	return users
}

func CountUsers(db *gorm.DB) int {
	var count int64
	db.Model(&User{}).Count(&count)
	return int(count)
}

func FindUserAdmins(db *gorm.DB) []User {
	var users []User
	db.Preload("Repeaters").Where("admin = ?", true).Find(&users)
	return users
}

func CountUserAdmins(db *gorm.DB) int {
	var count int64
	db.Model(&User{}).Where("admin = ?", true).Count(&count)
	return int(count)
}

func FindUserSuspended(db *gorm.DB) []User {
	var users []User
	db.Preload("Repeaters").Where("suspended = ?", true).Find(&users)
	return users
}

func CountUserSuspended(db *gorm.DB) int {
	var count int64
	db.Model(&User{}).Where("suspended = ?", true).Count(&count)
	return int(count)
}

func FindUserUnapproved(db *gorm.DB) []User {
	var users []User
	db.Preload("Repeaters").Where("approved = ?", false).Find(&users)
	return users
}

func CountUserUnapproved(db *gorm.DB) int {
	var count int64
	db.Model(&User{}).Where("approved = ?", false).Count(&count)
	return int(count)
}

type UsersSeeder struct {
	gorm_seeder.SeederAbstract
}

func NewUsersSeeder(cfg gorm_seeder.SeederConfiguration) UsersSeeder {
	return UsersSeeder{gorm_seeder.NewSeederAbstract(cfg)}
}

func (s *UsersSeeder) Seed(db *gorm.DB) error {
	var users = []User{
		{
			ID:       uint(9990),
			Callsign: "Parrot",
			Admin:    false,
			Approved: true,
		},
		{
			ID:       999999,
			Callsign: "SystemAdmin",
			Username: "Admin",
			Admin:    true,
			Approved: true,
			Password: utils.HashPassword(config.GetConfig().InitialAdminUserPassword, config.GetConfig().PasswordSalt),
		},
	}
	klog.Errorf("!#!#!#!#!# Initial admin user password: %s #!#!#!#!#!", config.GetConfig().InitialAdminUserPassword)
	return db.CreateInBatches(users, s.Configuration.Rows).Error
}

func (s *UsersSeeder) Clear(db *gorm.DB) error {
	return nil
}

func DeleteUser(db *gorm.DB, id uint) {
	db.Transaction(func(tx *gorm.DB) error {
		var repeaters []Repeater
		tx.Where("owner_id = ?", id).Find(&repeaters)
		for _, repeater := range repeaters {
			tx.Unscoped().Where("(is_to_repeater = ? AND to_repeater_id = ?) OR repeater_id = ?", true, repeater.RadioID, repeater.RadioID).Delete(&Call{})
			tx.Unscoped().Select(clause.Associations, "TS1StaticTalkgroups").Select(clause.Associations, "TS2StaticTalkgroups").Delete(&Repeater{RadioID: id})
			tx.Unscoped().Table("talkgroup_admins").Where("user_id = ?", id).Delete(&Talkgroup{})
			tx.Unscoped().Table("talkgroup_ncos").Where("user_id = ?", id).Delete(&Talkgroup{})
		}
		tx.Unscoped().Select(clause.Associations, "Repeaters").Delete(&User{ID: id})
		return nil
	})
}
