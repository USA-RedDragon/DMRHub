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
	Repeaters []Repeater     `gorm:"foreignKey:OwnerID"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `gorm:"index" json:"-"`
}

func UserExists(db *gorm.DB, user User) bool {
	var count int64
	db.Model(&User{}).Where("ID = ?", user.ID).Limit(1).Count(&count)
	return count > 0
}

func FindUserByID(db *gorm.DB, ID uint) User {
	var user User
	db.First(&user, ID)
	return user
}
