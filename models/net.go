package models

import (
	"time"

	"gorm.io/gorm"
)

type Net struct {
	ID          uint           `json:"-" gorm:"primarykey"`
	TalkgroupID uint           `json:"-"`
	Talkgroup   uint           `json:"talkgroup" gorm:"foreignKey:TalkgroupID"`
	StartTime   time.Time      `json:"start_time"`
	Duration    time.Duration  `json:"duration"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	RRule       string         `json:"rrule"`
	CreatedAt   time.Time      `json:"-"`
	UpdatedAt   time.Time      `json:"-"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
