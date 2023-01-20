package dmr

import (
	"time"

	"github.com/USA-RedDragon/dmrserver-in-a-box/models"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

// Assuming +/-7ms of jitter, we'll wait 2 seconds before we consider a call to be over
// This equates out to about 30 lost voice packets
const timerDelay = 2 * time.Second

type CallTracker struct {
	DB            *gorm.DB
	CallEndTimers map[uint]*time.Timer
}

func NewCallTracker(db *gorm.DB) *CallTracker {
	return &CallTracker{
		DB:            db,
		CallEndTimers: make(map[uint]*time.Timer),
	}
}

func (c *CallTracker) StartCall(packet models.Packet) {
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

	c.DB.Create(&call)
	if c.DB.Error != nil {
		klog.Errorf("Error creating call: %v", c.DB.Error)
		return
	}

	klog.Infof("Started call %d", call.StreamID)

	// Add a timer that will end the call if we haven't seen a packet in 1 second.
	c.CallEndTimers[call.ID] = time.AfterFunc(timerDelay, endCallHandler(c, packet))
}

func (c *CallTracker) IsCallActive(packet models.Packet) bool {
	if !models.ActiveCallExists(c.DB, packet.StreamId, packet.Src, packet.Dst, packet.Slot, packet.GroupCall) {
		klog.Errorf("Error finding active call: %v", packet.StreamId)
		return false
	}
	return true
}

func (c *CallTracker) ProcessCallPacket(packet models.Packet) {
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	call, err := models.FindActiveCall(c.DB, packet.StreamId, packet.Src, packet.Dst, packet.Slot, packet.GroupCall)
	if err != nil {
		klog.Errorf("Error finding active call: %v", err)
		return
	}

	// Reset call end timer
	c.CallEndTimers[call.ID].Reset(2 * time.Second)

	elapsed := time.Since(call.LastPacketTime)
	// call.Jitter is a float32 that represents how many ms off from 60ms elapsed
	// time the last packet was. We'll use this to calculate the average jitter.
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-60)) / 2
	call.LastPacketTime = time.Now()

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

		go c.DB.Save(&call)
	}

	var lost uint
	// Update call.TotalPackets with 1 + the number of packets that have been lost since the last packet
	// The first packet of a call will have a FrameType of HBPF_DATA_SYNC and a DTypeOrVSeq of HBPF_SLT_VHEAD. This does not count towards the FrameNum, but we need to check the order
	if packet.FrameType == HBPF_DATA_SYNC && packet.DTypeOrVSeq == HBPF_SLT_VHEAD {
		// Voice header kicks off the call, so we need to set the FrameNum to 0
		call.HasHeader = true
		call.TotalPackets++
		// Save the db early so that we can query for it in a different goroutine
	} else if packet.FrameType == HBPF_VOICE_SYNC && packet.DTypeOrVSeq == 0 {
		// This is a voice sync packet, so we need to ensure that we already have a header and set the FrameNum to 0
		if !call.HasHeader {
			klog.Errorf("%d Voice sync packet without header", packet.DTypeOrVSeq)
			lost++
		}
		// If the last frame number is not equal to 5, then we've lost a packet
		if call.LastFrameNum != 5 {
			lost += 5 - call.LastFrameNum
		}
		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		klog.Infof("%d Voice sync - lost %d packets. Set LastFrameNum to %d. Total lost: %d", packet.DTypeOrVSeq, lost, call.LastFrameNum, call.LostSequences)
	} else if packet.FrameType == HBPF_VOICE && packet.DTypeOrVSeq > 0 && packet.DTypeOrVSeq < 5 {
		// These are voice packets, so check for a header and LastFrameNum == packet.DTypeOrVSeq+1
		if !call.HasHeader {
			klog.Errorf("%d Voice packet without header", packet.DTypeOrVSeq)
		}

		// If the last frame number is equal to 5, then either we've lost a number of packets, or we've lost a sync packet
		if call.LastFrameNum == 5 {
			// Detect a lost sync packet
			if packet.DTypeOrVSeq == 1 {
				klog.Infof("%d Voice - lost sync packet", packet.DTypeOrVSeq)
				lost++
			} else {
				lost += packet.DTypeOrVSeq - 1
				klog.Infof("%d Voice - lost %d packets. Total lost: %d", packet.DTypeOrVSeq, lost, call.LostSequences)
			}
		} else {
			// If the last frame number is not equal to the current frame number - 1, then we've lost a packet
			if call.LastFrameNum != 0 && call.LastFrameNum != packet.DTypeOrVSeq-1 {
				lost += packet.DTypeOrVSeq - call.LastFrameNum - 1
				klog.Infof("%d Voice - lost %d packets. LastFrame=%d. Frame=%d. Total lost: %d", packet.DTypeOrVSeq, lost, call.LastFrameNum, packet.DTypeOrVSeq, call.LostSequences)
			}
		}

		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		klog.Infof("%d Voice - lost %d packets Set LastFrameNum to %d. Total lost: %d", packet.DTypeOrVSeq, lost, call.LastFrameNum, call.LostSequences)
	} else if packet.FrameType == HBPF_VOICE && packet.DTypeOrVSeq == 5 {
		// This is the last voice packet, so check for a header and LastFrameNum == 4
		if !call.HasHeader {
			klog.Errorf("%d Voice packet without header", packet.DTypeOrVSeq)
		}
		// If the last frame number is not equal to 4, then we've lost a packet
		if call.LastFrameNum != 4 {
			lost += 4 - call.LastFrameNum
		}
		call.LastFrameNum = packet.DTypeOrVSeq
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		klog.Infof("%d Last voice - lost %d packets. Total lost: %d", packet.DTypeOrVSeq, lost, call.LostSequences)
	} else if packet.FrameType == HBPF_DATA_SYNC && packet.DTypeOrVSeq == HBPF_SLT_VTERM {
		// This is the end of a call, so we need to set the FrameNum to 0
		// Check if LastFrameNum is 5, if not, we've lost some packets
		if call.LastFrameNum != 5 {
			lost += 5 - call.LastFrameNum
		}
		call.HasTerm = true
		call.LastFrameNum = 5
		call.LostSequences += lost
		call.TotalPackets += 1 + lost
		klog.Infof("%d Voice termination - lost %d packets. Total lost: %d", packet.DTypeOrVSeq, lost, call.LostSequences)
	}

	call.Duration = time.Since(call.StartTime)
	call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)
	call.Active = true

	c.DB.Save(&call)
}

func endCallHandler(c *CallTracker, packet models.Packet) func() {
	return func() {
		klog.Errorf("Call %d timed out", packet.StreamId)
		c.EndCall(packet)
	}
}

func (c *CallTracker) EndCall(packet models.Packet) {
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	call, err := models.FindActiveCall(c.DB, packet.StreamId, packet.Src, packet.Dst, packet.Slot, packet.GroupCall)
	if err != nil {
		klog.Errorf("Error finding active call: %v", err)
		return
	}
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
		klog.Errorf("Call %d ended without a term", packet.StreamId)
	}

	// If lastFrameNum != 5, Calculate the number of lost packets by subtracting the last frame number from 5 and adding it to the lost sequences
	if call.LastFrameNum != 5 {
		call.LostSequences += 5 - call.LastFrameNum
		call.TotalPackets += 5 - call.LastFrameNum
		klog.Errorf("Call %d ended with %d lost packets", packet.StreamId, 5-call.LastFrameNum)
	}

	call.Active = false
	call.Duration = time.Since(call.StartTime)
	call.Loss = float32(call.LostSequences / call.TotalPackets)
	c.DB.Save(&call)

	klog.Errorf("Call %d ended with duration %v, %f percent loss, and %f Jitter", packet.StreamId, call.Duration, call.Loss*100, call.Jitter)
}
