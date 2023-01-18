package models

import (
	"time"

	"gorm.io/gorm"
)

type Call struct {
	ID             uint           `json:"-" gorm:"primarykey"`
	StreamID       uint           `json:"stream_id"`
	StartTime      time.Time      `json:"-"`
	Duration       time.Duration  `json:"duration"`
	Active         bool           `json:"active"`
	User           User           `json:"user" gorm:"foreignKey:UserID"`
	UserID         uint           `json:"-"`
	Repeater       Repeater       `json:"repeater" gorm:"foreignKey:RepeaterID"`
	RepeaterID     uint           `json:"-"`
	TimeSlot       bool           `json:"time_slot"`
	GroupCall      bool           `json:"group_call"`
	IsToTalkgroup  bool           `json:"is_to_talkgroup"`
	ToTalkgroupID  uint           `json:"-"`
	ToTalkgroup    Talkgroup      `json:"to_talkgroup" gorm:"foreignKey:ToTalkgroupID"`
	IsToUser       bool           `json:"is_to_user"`
	ToUserID       uint           `json:"-"`
	ToUser         User           `json:"to_user" gorm:"foreignKey:ToUserID"`
	IsToRepeater   bool           `json:"is_to_repeater"`
	ToRepeaterID   uint           `json:"-"`
	ToRepeater     Repeater       `json:"to_repeater" gorm:"foreignKey:ToRepeaterID"`
	DestinationID  uint           `json:"destination_id"`
	TotalPackets   uint           `json:"total_packets"`
	LostSequences  uint           `json:"lost_sequences"`
	LastSeq        uint           `json:"-"`
	Loss           float32        `json:"loss"`
	Jitter         float32        `json:"jitter"`
	LastPacketTime time.Time      `json:"-"`
	CreatedAt      time.Time      `json:"created_at" msg:"-"`
	UpdatedAt      time.Time      `json:"-" msg:"-"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index" msg:"-"`
}

func FindActiveCall(db *gorm.DB, streamID uint, src uint, dst uint, slot bool, groupCall bool) (Call, error) {
	var call Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("stream_id = ? AND active = ? AND user_id = ? AND destination_id = ? AND time_slot = ? AND group_call = ?", streamID, true, src, dst, slot, groupCall).First(&call)
	if db.Error != nil {
		return call, db.Error
	}
	return call, nil
}

func ActiveCallExists(db *gorm.DB, streamID uint, src uint, dst uint, slot bool, groupCall bool) bool {
	var count int64
	db.Model(&Call{}).Where("stream_id = ? AND active = ? AND user_id = ? AND destination_id = ? AND time_slot = ? AND group_call = ?", streamID, true, src, dst, slot, groupCall).Count(&count)
	return count > 0
}
