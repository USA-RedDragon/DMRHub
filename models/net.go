package models

import (
	"time"

	"gorm.io/gorm"
)

// Basically a calendar event. We should be able to use the data here to create DB queries to find calls during a given time period for the given talkgroup.
// Net has a talkgroup id, name, description, time, duration, and whatever data is needed to create recurring events (such as bi-weekly, first and third Tuesday of the month, etc.)

type Net struct {
	ID          uint           `json:"-" gorm:"primarykey"`
	TalkgroupID uint           `json:"-"`
	Talkgroup   uint           `json:"talkgroup" gorm:"foreignKey:TalkgroupID"`
	StartTime   time.Time      `json:"start_time"`
	Duration    time.Duration  `json:"duration"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	CreatedAt   time.Time      `json:"-"`
	UpdatedAt   time.Time      `json:"-"`
	DeletedAt   gorm.DeletedAt `json:"-" gorm:"index"`
}
