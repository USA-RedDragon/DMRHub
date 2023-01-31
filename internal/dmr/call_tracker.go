package dmr

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/models"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

// Assuming +/-7ms of jitter, we'll wait 2 seconds before we consider a call to be over
// This equates out to about 30 lost voice packets
const timerDelay = 2 * time.Second

type CallTracker struct {
	DB            *gorm.DB
	Redis         *redis.Client
	CallEndTimers map[uint]*time.Timer
	InFlightCalls map[uint]*models.Call
}

func NewCallTracker(db *gorm.DB, redis *redis.Client) *CallTracker {
	return &CallTracker{
		DB:            db,
		Redis:         redis,
		CallEndTimers: make(map[uint]*time.Timer),
		InFlightCalls: make(map[uint]*models.Call),
	}
}

func (c *CallTracker) StartCall(ctx context.Context, packet models.Packet) {
	var sourceUser models.User
	var sourceRepeater models.Repeater

	if !models.UserIDExists(c.DB, packet.Src) {
		klog.Errorf("User %d does not exist", packet.Src)
		return
	}
	sourceUser = models.FindUserByID(c.DB, packet.Src)

	if !models.RepeaterIDExists(c.DB, packet.Repeater) {
		klog.Errorf("Repeater %d does not exist", packet.Repeater)
		return
	}
	sourceRepeater = models.FindRepeaterByID(c.DB, packet.Repeater)

	isToRepeater, isToTalkgroup, isToUser := false, false, false
	var destUser models.User
	var destRepeater models.Repeater
	var destTalkgroup models.Talkgroup

	// Try the different targets for the given call destination
	// if packet.GroupCall is true, then packet.Dst is either a talkgroup or a repeater
	// if packet.GroupCall is false, then packet.Dst is a user
	if packet.GroupCall {
		// Decide between talkgroup and repeater
		if !models.TalkgroupIDExists(c.DB, packet.Dst) {
			if !models.RepeaterIDExists(c.DB, packet.Dst) {
				klog.Errorf("Cannot find packet destination %d", packet.Dst)
				return
			} else {
				isToRepeater = true
				destRepeater = models.FindRepeaterByID(c.DB, packet.Dst)
			}
		} else {
			isToTalkgroup = true
			destTalkgroup = models.FindTalkgroupByID(c.DB, packet.Dst)
		}
	} else {
		// Find the user
		if !models.UserIDExists(c.DB, packet.Dst) {
			klog.Errorf("Cannot find packet destination %d", packet.Dst)
			return
		} else {
			isToUser = true
			destUser = models.FindUserByID(c.DB, packet.Dst)
		}
	}

	call := models.Call{
		StreamID:       packet.StreamId,
		StartTime:      time.Now(),
		Active:         true,
		User:           sourceUser,
		UserID:         sourceUser.ID,
		Repeater:       sourceRepeater,
		RepeaterID:     sourceRepeater.RadioID,
		TimeSlot:       packet.Slot,
		GroupCall:      packet.GroupCall,
		DestinationID:  packet.Dst,
		TotalPackets:   0,
		LostSequences:  0,
		LastPacketTime: time.Now(),
		Loss:           0.0,
		Jitter:         0.0,
		LastFrameNum:   5,
		RSSI:           0,
		BER:            0.0,
		TotalBits:      0,
		HasHeader:      false,
		HasTerm:        false,
	}

	call.IsToRepeater = isToRepeater
	call.IsToUser = isToUser
	call.IsToTalkgroup = isToTalkgroup
	if isToRepeater {
		call.ToRepeater = destRepeater
	} else if isToUser {
		call.ToUser = destUser
	} else if isToTalkgroup {
		call.ToTalkgroup = destTalkgroup
	}

	// Create the call in the database
	c.DB.Create(&call)
	if c.DB.Error != nil {
		klog.Errorf("Error creating call: %v", c.DB.Error)
		return
	}

	// Add the call to the active calls map
	c.InFlightCalls[call.ID] = &call

	if config.GetConfig().Debug {
		klog.Infof("Started call %d", call.StreamID)
	}

	// Add a timer that will end the call if we haven't seen a packet in 1 second.
	c.CallEndTimers[call.ID] = time.AfterFunc(timerDelay, endCallHandler(ctx, c, packet))
}

func (c *CallTracker) IsCallActive(packet models.Packet) bool {
	for _, call := range c.InFlightCalls {
		if call.Active && call.StreamID == packet.StreamId && call.UserID == packet.Src && call.DestinationID == packet.Dst && call.TimeSlot == packet.Slot && call.GroupCall == packet.GroupCall {
			return true
		}
	}
	return false
}

type jsonCallResponseUser struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type jsonCallResponseRepeater struct {
	RadioID  uint   `json:"radio_id"`
	Callsign string `json:"callsign"`
}

type jsonCallResponseTalkgroup struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type jsonCallResponse struct {
	ID            uint                      `json:"id"`
	User          jsonCallResponseUser      `json:"user"`
	StartTime     time.Time                 `json:"start_time"`
	Duration      time.Duration             `json:"duration"`
	Active        bool                      `json:"active"`
	TimeSlot      bool                      `json:"time_slot"`
	GroupCall     bool                      `json:"group_call"`
	IsToTalkgroup bool                      `json:"is_to_talkgroup"`
	ToTalkgroup   jsonCallResponseTalkgroup `json:"to_talkgroup"`
	IsToUser      bool                      `json:"is_to_user"`
	ToUser        jsonCallResponseUser      `json:"to_user"`
	IsToRepeater  bool                      `json:"is_to_repeater"`
	ToRepeater    jsonCallResponseRepeater  `json:"to_repeater"`
	Loss          float32                   `json:"loss"`
	Jitter        float32                   `json:"jitter"`
	BER           float32                   `json:"ber"`
	RSSI          float32                   `json:"rssi"`
}

func (c *CallTracker) publishCall(ctx context.Context, call *models.Call, packet models.Packet) {
	// copy call into a jsonCallResponse
	var jsonCall jsonCallResponse
	jsonCall.ID = call.ID
	jsonCall.User.ID = call.User.ID
	jsonCall.User.Callsign = call.User.Callsign
	jsonCall.StartTime = call.StartTime
	jsonCall.Duration = call.Duration
	jsonCall.Active = call.Active
	jsonCall.TimeSlot = call.TimeSlot
	jsonCall.GroupCall = call.GroupCall
	jsonCall.IsToTalkgroup = call.IsToTalkgroup
	jsonCall.ToTalkgroup.ID = call.ToTalkgroup.ID
	jsonCall.ToTalkgroup.Name = call.ToTalkgroup.Name
	jsonCall.ToTalkgroup.Description = call.ToTalkgroup.Description
	jsonCall.IsToUser = call.IsToUser
	jsonCall.ToUser.ID = call.ToUser.ID
	jsonCall.ToUser.Callsign = call.ToUser.Callsign
	jsonCall.IsToRepeater = call.IsToRepeater
	jsonCall.ToRepeater.RadioID = call.ToRepeater.RadioID
	jsonCall.ToRepeater.Callsign = call.ToRepeater.Callsign
	jsonCall.Loss = call.Loss
	jsonCall.Jitter = call.Jitter
	jsonCall.BER = call.BER
	jsonCall.RSSI = call.RSSI
	// Publish the call JSON to Redis
	var callJSON []byte
	callJSON, err := json.Marshal(jsonCall)
	if err != nil {
		klog.Errorf("Error marshalling call JSON: %v", err)
		return
	}
	// Save for hoseline:
	if (call.IsToRepeater || call.IsToTalkgroup) && call.GroupCall {
		_, err = c.Redis.Publish(ctx, "calls", callJSON).Result()
		if err != nil {
			klog.Errorf("Error publishing call JSON: %v", err)
			return
		}
	}

	// Iterate all repeaters to see if they want the call
	var repeaters []models.Repeater
	alreadyPublished := make(map[uint]bool)
	c.DB.Preload("Owner").Find(&repeaters)
	for _, repeater := range repeaters {
		want, _ := repeater.WantRX(packet)
		if want && !alreadyPublished[repeater.OwnerID] {
			// Publish the call to the repeater owner's call history
			c.Redis.Publish(ctx, fmt.Sprintf("calls:%d", repeater.OwnerID), callJSON)
			alreadyPublished[repeater.OwnerID] = true
			continue
		}
		if repeater.OwnerID == call.UserID {
			c.Redis.Publish(ctx, fmt.Sprintf("calls:%d", repeater.OwnerID), callJSON)
			alreadyPublished[repeater.OwnerID] = true
		}
	}
}

func (c *CallTracker) updateCall(ctx context.Context, call *models.Call, packet models.Packet) {
	// Reset call end timer
	c.CallEndTimers[call.ID].Reset(2 * time.Second)

	elapsed := time.Since(call.LastPacketTime)
	call.LastPacketTime = time.Now()
	// call.Jitter is a float32 that represents how many ms off from 60ms elapsed
	// time the last packet was. We'll use this to calculate the average jitter.
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-60)) / 2

	if call.LastFrameNum != 0 && call.LastFrameNum == packet.DTypeOrVSeq {
		// We've already seen this packet, so it's either a duplicate or we've lost 6 packets
		if time.Since(call.LastPacketTime) > 60*time.Millisecond {
			// We've lost 6 packets
			call.LostSequences++
			call.TotalPackets += 6
		} else {
			// We've received a duplicate packet
			call.TotalPackets++
		}
		call.Duration = time.Since(call.StartTime)
		call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)
		call.Active = true
		if packet.RSSI > 0 {
			call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2
		}

		if call.TotalPackets%2 == 0 {
			go c.publishCall(ctx, call, packet)
		}
		return
	}

	var lost uint
	// Update call.TotalPackets with 1 + the number of packets that have been lost since the last packet
	// The first packet of a call will have a FrameType of HBPF_DATA_SYNC and a DTypeOrVSeq of HBPF_SLT_VHEAD. This does not count towards the FrameNum, but we need to check the order
	if packet.FrameType == dmrconst.FRAME_DATA_SYNC && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTYPE_VOICE_HEAD {
		// Voice header kicks off the call, so we need to set the FrameNum to 0
		call.HasHeader = true
		call.TotalPackets++
		// Save the db early so that we can query for it in a different goroutine
	} else if packet.FrameType == dmrconst.FRAME_VOICE_SYNC && packet.DTypeOrVSeq == 0 {
		// This is a voice sync packet, so we need to ensure that we already have a header and set the FrameNum to 0
		if !call.HasHeader {
			klog.Error("Voice sync packet without header")
			lost++
		}
		// If the last frame number is not equal to 5, then we've lost a packet
		if call.LastFrameNum != 5 {
			lost += 5 - call.LastFrameNum
		}

		if packet.BER > 0 {
			call.TotalBits += 141
			call.BER = ((call.BER + float32(packet.BER)) / float32(call.TotalBits)) / 2
		}
		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		if config.GetConfig().Debug {
			klog.Infof("Voice sync - lost %d packets. Set LastFrameNum to %d. Total lost: %d", lost, call.LastFrameNum, call.LostSequences)
		}
	} else if packet.FrameType == dmrconst.FRAME_VOICE && packet.DTypeOrVSeq > 0 && packet.DTypeOrVSeq < 5 {
		// These are voice packets, so check for a header and LastFrameNum == packet.DTypeOrVSeq+1
		if !call.HasHeader {
			klog.Error("Voice packet without header")
		}

		// If the last frame number is equal to 5, then either we've lost a number of packets, or we've lost a sync packet
		if call.LastFrameNum == 5 {
			// Detect a lost sync packet
			if packet.DTypeOrVSeq == 1 {
				if config.GetConfig().Debug {
					klog.Infof("Voice - lost sync packet", packet.DTypeOrVSeq)
				}
				lost++
			} else {
				lost += packet.DTypeOrVSeq - 1
				if config.GetConfig().Debug {
					klog.Infof("Voice - lost %d packets. Total lost: %d", lost, call.LostSequences)
				}
			}
		} else {
			// If the last frame number is not equal to the current frame number - 1, then we've lost a packet
			if call.LastFrameNum != 0 && call.LastFrameNum != packet.DTypeOrVSeq-1 {
				lost += packet.DTypeOrVSeq - call.LastFrameNum - 1
				if config.GetConfig().Debug {
					klog.Infof("Voice - lost %d packets. LastFrame=%d. Frame=%d. Total lost: %d", lost, call.LastFrameNum, packet.DTypeOrVSeq, call.LostSequences)
				}
			}
		}

		if packet.BER > 0 {
			call.TotalBits += 141
			call.BER = ((call.BER + float32(packet.BER)) / float32(call.TotalBits)) / 2
		}
		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		if config.GetConfig().Debug {
			klog.Infof("Voice - lost %d packets Set LastFrameNum to %d. Total lost: %d", lost, call.LastFrameNum, call.LostSequences)
		}
	} else if packet.FrameType == dmrconst.FRAME_VOICE && packet.DTypeOrVSeq == 5 {
		// This is the last voice packet, so check for a header and LastFrameNum == 4
		if !call.HasHeader {
			klog.Errorf("Voice packet without header", packet.DTypeOrVSeq)
		}
		// If the last frame number is not equal to 4, then we've lost a packet
		if call.LastFrameNum != 4 {
			lost += 4 - call.LastFrameNum
		}
		if packet.BER > 0 {
			call.TotalBits += 141
			call.BER = ((call.BER + float32(packet.BER)) / float32(call.TotalBits)) / 2
		}
		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		if config.GetConfig().Debug {
			klog.Infof("Last voice - lost %d packets. Total lost: %d", lost, call.LostSequences)
		}
	} else if packet.FrameType == dmrconst.FRAME_DATA_SYNC && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTYPE_VOICE_TERM {
		// This is the end of a call, so we need to set the FrameNum to 0
		// Check if LastFrameNum is 5, if not, we've lost some packets
		if call.LastFrameNum != 5 {
			lost += 5 - call.LastFrameNum
		}
		call.HasTerm = true
		call.LastFrameNum = 5
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		if config.GetConfig().Debug {
			klog.Infof("Voice termination - lost %d packets. Total lost: %d", lost, call.LostSequences)
		}
	}

	call.Duration = time.Since(call.StartTime)
	call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)
	call.Active = true
	if packet.RSSI > 0 {
		call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2
	}

	if call.TotalPackets%2 == 0 {
		go c.publishCall(ctx, call, packet)
	}
}

func (c *CallTracker) ProcessCallPacket(ctx context.Context, packet models.Packet) {
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	for _, lcall := range c.InFlightCalls {
		if lcall.StreamID == packet.StreamId && lcall.Active && lcall.TimeSlot == packet.Slot && lcall.GroupCall == packet.GroupCall && lcall.UserID == packet.Src {
			c.updateCall(ctx, lcall, packet)
			return
		}
	}
}

func endCallHandler(ctx context.Context, c *CallTracker, packet models.Packet) func() {
	return func() {
		klog.Errorf("Call %d timed out", packet.StreamId)
		c.EndCall(ctx, packet)
	}
}

func (c *CallTracker) EndCall(ctx context.Context, packet models.Packet) {
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	for _, call := range c.InFlightCalls {
		if call.StreamID == packet.StreamId && call.Active && call.TimeSlot == packet.Slot && call.GroupCall == packet.GroupCall && call.UserID == packet.Src {
			// Delete the call end timer
			timer := c.CallEndTimers[call.ID]
			timer.Stop()
			delete(c.CallEndTimers, call.ID)

			if time.Since(call.StartTime) < 100*time.Millisecond {
				// This is probably a key-up, so delete the call from the db
				c.DB.Delete(&call)
				return
			}

			// If the call doesn't have a term, we lost that packet
			if !call.HasTerm {
				call.LostSequences++
				call.TotalPackets++
				if config.GetConfig().Debug {
					klog.Errorf("Call %d ended without a term", packet.StreamId)
				}
			}

			// If lastFrameNum != 5, Calculate the number of lost packets by subtracting the last frame number from 5 and adding it to the lost sequences
			if call.LastFrameNum != 5 {
				call.LostSequences += 5 - call.LastFrameNum
				call.TotalPackets += 5 - call.LastFrameNum
				if config.GetConfig().Debug {
					klog.Errorf("Call %d ended with %d lost packets", packet.StreamId, 5-call.LastFrameNum)
				}
			}

			call.Active = false
			call.Duration = time.Since(call.StartTime)
			call.Loss = float32(call.LostSequences / call.TotalPackets)
			c.DB.Save(call)
			c.publishCall(ctx, call, packet)
			delete(c.InFlightCalls, call.ID)

			klog.Infof("Call %d from %d to %d via %d ended with duration %v, %f%% Loss, %f%% BER, %fdBm RSSI, and %fms Jitter", packet.StreamId, packet.Src, packet.Dst, packet.Repeater, call.Duration, call.Loss*100, call.BER*100, call.RSSI, call.Jitter)
		}
	}
}
