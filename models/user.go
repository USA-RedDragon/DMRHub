package models

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	ID        uint           `json:"id" gorm:"primaryKey" binding:"required"`
	Callsign  string         `json:"callsign" gorm:"uniqueIndex" binding:"required"`
	Username  string         `json:"username" gorm:"uniqueIndex" binding:"required"`
	Password  string         `json:"-"`
	Admin     bool           `json:"admin"`
	Approved  bool           `json:"approved" binding:"required"`
	Repeaters []Repeater     `json:"repeaters" gorm:"foreignKey:OwnerID"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
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
