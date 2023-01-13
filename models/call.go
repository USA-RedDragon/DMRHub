package models

import (
	"time"

	"gorm.io/gorm"
)

type Call struct {
	gorm.Model
	StartTime   time.Time
	EndTime     time.Time
	Active      bool
	User        User
	UserID      uint
	Repeater    Repeater
	RepeaterID  uint
	TimeSlot    bool
	GroupCall   bool
	Talkgroup   Talkgroup
	TalkgroupID uint
	Destination int
}
