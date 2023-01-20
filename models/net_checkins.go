package models

import (
	"time"

	"gorm.io/gorm"
)

// Has one net. Contains many calls from who checked in. Contains start and end date and time.
type NetCheckins struct {
	ID        uint           `json:"-" gorm:"primarykey"`
	NetID     uint           `json:"-"`
	Net       Net            `json:"talkgroup" gorm:"foreignKey:NetID"`
	StartTime time.Time      `json:"start_time"`
	Duration  time.Duration  `json:"duration"`
	Checkins  []Call         `json:"checkins" gorm:"many2many:net_checkin_calls;"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}
