package models

import (
	"time"

	"gorm.io/gorm"
)

type NetEvent struct {
	ID        uint           `json:"-" gorm:"primarykey"`
	NetID     uint           `json:"-"`
	Net       Net            `json:"talkgroup" gorm:"foreignKey:NetID"`
	StartTime time.Time      `json:"start_time"`
	Duration  time.Duration  `json:"duration"`
	Checkins  []Call         `json:"checkins" gorm:"many2many:net_event_checkins;"`
	CreatedAt time.Time      `json:"-"`
	UpdatedAt time.Time      `json:"-"`
	DeletedAt gorm.DeletedAt `json:"-" gorm:"index"`
}
