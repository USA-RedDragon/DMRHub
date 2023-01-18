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

func FindCalls(db *gorm.DB, limit int) []Call {
	var calls []Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_talkgroup = ?", true).Order("start_time desc").Limit(limit).Find(&calls)
	return calls
}

func FindRepeaterCalls(db *gorm.DB, repeaterID uint, limit int) []Call {
	var toRepeaterCalls []Call
	var fromRepeaterCalls []Call
	// Find calls where (IsToRepeater is true and ToRepeaterID is repeaterID) or (RepeaterID is repeaterID)
	db.Preload("User").Preload("Repeater").Preload("ToRepeater").Where("is_to_repeater = ? AND to_repeater_id = ?", true, repeaterID).Order("start_time desc").Limit(limit).Find(&toRepeaterCalls)
	db.Preload("User").Preload("Repeater").Preload("ToRepeater").Where("repeater_id = ?", repeaterID).Order("start_time desc").Limit(limit).Find(&fromRepeaterCalls)
	// Merge the two slices in order of start time
	var i, j int
	var mergedCalls []Call
	for i < len(toRepeaterCalls) && j < len(fromRepeaterCalls) {
		if toRepeaterCalls[i].StartTime.After(fromRepeaterCalls[j].StartTime) {
			mergedCalls = append(mergedCalls, toRepeaterCalls[i])
			i++
		} else {
			mergedCalls = append(mergedCalls, fromRepeaterCalls[j])
			j++
		}
	}
	mergedCalls = append(mergedCalls, toRepeaterCalls[i:]...)
	mergedCalls = append(mergedCalls, fromRepeaterCalls[j:]...)
	return mergedCalls
}

func FindUserCalls(db *gorm.DB, userID uint, limit int) []Call {
	var toUserCalls []Call
	var fromUserCalls []Call
	// Find calls where (IsToUser is true and ToUserID is userID) or (UserID is userID)
	db.Preload("User").Preload("Repeater").Preload("ToUser").Where("is_to_user = ? AND to_user_id = ?", true, userID).Order("start_time desc").Limit(limit).Find(&toUserCalls)
	db.Preload("User").Preload("Repeater").Preload("ToUser").Where("user_id = ?", userID).Order("start_time desc").Limit(limit).Find(&fromUserCalls)
	// Merge the two slices in order of start time
	var i, j int
	var mergedCalls []Call
	for i < len(toUserCalls) && j < len(fromUserCalls) {
		if toUserCalls[i].StartTime.After(fromUserCalls[j].StartTime) {
			mergedCalls = append(mergedCalls, toUserCalls[i])
			i++
		} else {
			mergedCalls = append(mergedCalls, fromUserCalls[j])
			j++
		}
	}
	mergedCalls = append(mergedCalls, toUserCalls[i:]...)
	mergedCalls = append(mergedCalls, fromUserCalls[j:]...)

	return mergedCalls
}

func FindTalkgroupCalls(db *gorm.DB, talkgroupID uint, limit int) []Call {
	var calls []Call
	// Find calls where (IsToTalkgroup is true and ToTalkgroupID is talkgroupID)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Where("is_to_talkgroup = ? AND to_talkgroup_id = ?", true, talkgroupID).Order("start_time desc").Limit(limit).Find(&calls)
	return calls
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
