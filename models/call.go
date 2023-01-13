package models

import (
	"time"

	"gorm.io/gorm"
)

type Call struct {
	gorm.Model
	Time        time.Time
	Duration    time.Duration
	User        User
	Repeater    Repeater
	TimeSlot    bool
	GroupCall   bool
	Talkgroup   Talkgroup
	Destination int
}
