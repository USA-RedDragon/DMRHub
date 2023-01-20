package models

import (
	"time"

	orderedmap "github.com/wk8/go-ordered-map"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

type Call struct {
	ID             uint           `json:"-" gorm:"primarykey"`
	StreamID       uint           `json:"-"`
	StartTime      time.Time      `json:"start_time"`
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
	Loss           float32        `json:"loss"`
	Jitter         float32        `json:"jitter"`
	LastFrameNum   uint           `json:"-"`
	BER            float32        `json:"-"`
	RSSI           float32        `json:"-"`
	TotalBits      uint           `json:"-"`
	LastPacketTime time.Time      `json:"-"`
	HasHeader      bool           `json:"-"`
	HasTerm        bool           `json:"-"`
	CreatedAt      time.Time      `json:"-"`
	UpdatedAt      time.Time      `json:"-"`
	DeletedAt      gorm.DeletedAt `json:"-" gorm:"index`
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
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_repeater = ? AND to_repeater_id = ?", true, repeaterID).Order("start_time desc").Limit(limit).Find(&toRepeaterCalls)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("repeater_id = ?", repeaterID).Order("start_time desc").Limit(limit).Find(&fromRepeaterCalls)
	// Find calls where (IsToTalkgroup is true and either Repeater.TS1StaticTalkgroups, TS2StaticTalkgroups, TS1DynamicTalkgroupID, or TS2DynamicTalkgroupID contains talkgroup)
	var talkgroupCalls []Call
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_talkgroup = ?", true).Order("start_time desc").Limit(limit).Find(&talkgroupCalls)
	for _, call := range talkgroupCalls {
		if call.Repeater.TS1StaticTalkgroups != nil {
			for _, talkgroup := range call.Repeater.TS1StaticTalkgroups {
				if talkgroup.ID == call.ToTalkgroup.ID {
					fromRepeaterCalls = append(fromRepeaterCalls, call)
					continue
				}
			}
		}
		if call.Repeater.TS2StaticTalkgroups != nil {
			for _, talkgroup := range call.Repeater.TS2StaticTalkgroups {
				if talkgroup.ID == call.ToTalkgroup.ID {
					fromRepeaterCalls = append(fromRepeaterCalls, call)
					continue
				}
			}
		}
		if call.Repeater.TS1DynamicTalkgroupID == call.ToTalkgroup.ID {
			fromRepeaterCalls = append(fromRepeaterCalls, call)
			continue
		}
		if call.Repeater.TS2DynamicTalkgroupID == call.ToTalkgroup.ID {
			fromRepeaterCalls = append(fromRepeaterCalls, call)
			continue
		}
	}
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

	// Remove duplicates and take the first limit
	var uniqueCalls []Call
	uniqueCallsMap := orderedmap.New()
	for _, call := range mergedCalls {
		if _, exists := uniqueCallsMap.Get(call.ID); !exists {
			uniqueCallsMap.Set(call.ID, call)
		}
	}
	l := 0
	for pair := uniqueCallsMap.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			klog.Errorf("Call with index %d does not exist", pair.Key)
			continue
		}
		call := pair.Value.(Call)
		uniqueCalls = append(uniqueCalls, call)
		l++
		if l >= limit {
			break
		}
	}

	return uniqueCalls
}

func FindUserCalls(db *gorm.DB, userID uint, limit int) []Call {
	var toUserCalls []Call
	var fromUserCalls []Call
	// Find calls where (IsToUser is true and ToUserID is userID) or (UserID is userID) or (IsToTalkgroup and User.Repeaters contains repeaters that listen to talkgroup)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_user = ? AND to_user_id = ?", true, userID).Order("start_time desc").Limit(limit).Find(&toUserCalls)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("user_id = ?", userID).Order("start_time desc").Limit(limit).Find(&fromUserCalls)
	// Find calls where (IsToTalkgroup is true and User.Repeaters contains repeaters that listen to talkgroup)
	// Get the user
	user := FindUserByID(db, userID)
	// For each user.Repeaters
	for _, repeater := range user.Repeaters {
		fromUserCalls = append(fromUserCalls, FindRepeaterCalls(db, repeater.RadioID, limit)...)
	}
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

	// Remove duplicates and take the first limit
	var uniqueCalls []Call
	uniqueCallsMap := orderedmap.New()
	for _, call := range mergedCalls {
		if _, exists := uniqueCallsMap.Get(call.ID); !exists {
			uniqueCallsMap.Set(call.ID, call)
		}
	}
	l := 0
	for pair := uniqueCallsMap.Oldest(); pair != nil; pair = pair.Next() {
		if pair.Value == nil {
			klog.Errorf("Call with index %d does not exist", pair.Key)
			continue
		}
		call := pair.Value.(Call)
		uniqueCalls = append(uniqueCalls, call)
		l++
		if l >= limit {
			break
		}
	}

	return uniqueCalls
}

func FindTalkgroupCalls(db *gorm.DB, talkgroupID uint, limit int) []Call {
	var calls []Call
	// Find calls where (IsToTalkgroup is true and ToTalkgroupID is talkgroupID)
	db.Preload("User").Preload("Repeater").Preload("ToTalkgroup").Preload("ToUser").Preload("ToRepeater").Where("is_to_talkgroup = ? AND to_talkgroup_id = ?", true, talkgroupID).Order("start_time desc").Limit(limit).Find(&calls)
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
