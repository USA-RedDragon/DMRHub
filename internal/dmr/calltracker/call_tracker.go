// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU Affero General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU Affero General Public License for more details.
//
// You should have received a copy of the GNU Affero General Public License
// along with this program. If not, see <https://www.gnu.org/licenses/>.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package calltracker

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

// Assuming +/-7ms of jitter, we'll wait 2 seconds before we consider a call to be over
// This equates out to about 30 lost voice packets.
const timerDelay = 2 * time.Second
const packetTimingMs = 60
const pct = 100

// CallTracker is a struct that holds the state of the calls that are currently in progress.
type CallTracker struct {
	db                 *gorm.DB
	redis              *redis.Client
	callEndTimers      map[uint]*time.Timer
	callEndTimersMutex sync.RWMutex
	inFlightCalls      map[uint]*models.Call
	inFlightCallsMutex sync.RWMutex
}

// NewCallTracker creates a new CallTracker.
func NewCallTracker(db *gorm.DB, redis *redis.Client) *CallTracker {
	return &CallTracker{
		db:            db,
		redis:         redis,
		callEndTimers: make(map[uint]*time.Timer),
		inFlightCalls: make(map[uint]*models.Call),
	}
}

// StartCall starts tracking a new call.
func (c *CallTracker) StartCall(ctx context.Context, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.StartCall")
	defer span.End()

	var sourceUser models.User
	var sourceRepeater models.Repeater

	userExists, err := models.UserIDExists(c.db, packet.Src)
	if err != nil {
		klog.Errorf("Error checking if user %d exists: %s", packet.Src, err)
		return
	}

	if !userExists {
		if config.GetConfig().Debug {
			klog.Errorf("User %d does not exist", packet.Src)
		}
		return
	}

	sourceUser, err = models.FindUserByID(c.db, packet.Src)
	if err != nil {
		klog.Errorf("Error finding user %d: %s", packet.Src, err)
		return
	}

	repeaterExists, err := models.RepeaterIDExists(c.db, packet.Repeater)
	if err != nil {
		klog.Errorf("Error checking if repeater %d exists: %s", packet.Repeater, err)
		return
	}

	if !repeaterExists {
		if config.GetConfig().Debug {
			klog.Errorf("Repeater %d does not exist", packet.Repeater)
		}
		return
	}

	sourceRepeater, err = models.FindRepeaterByID(c.db, packet.Repeater)
	if err != nil {
		klog.Errorf("Error finding repeater %d: %s", packet.Repeater, err)
		return
	}

	isToRepeater, isToTalkgroup, isToUser := false, false, false
	var destUser models.User
	var destRepeater models.Repeater
	var destTalkgroup models.Talkgroup

	// Try the different targets for the given call destination
	// if packet.GroupCall is true, then packet.Dst is either a talkgroup or a repeater
	// if packet.GroupCall is false, then packet.Dst is a user
	if packet.GroupCall {
		talkgroupExists, err := models.TalkgroupIDExists(c.db, packet.Dst)
		if err != nil {
			klog.Errorf("Error checking if talkgroup %d exists: %s", packet.Dst, err)
			return
		}

		repeaterExists, err := models.RepeaterIDExists(c.db, packet.Dst)
		if err != nil {
			klog.Errorf("Error checking if repeater %d exists: %s", packet.Dst, err)
			return
		}

		switch {
		case talkgroupExists:
			isToTalkgroup = true
			destTalkgroup, err = models.FindTalkgroupByID(c.db, packet.Dst)
			if err != nil {
				klog.Errorf("Error finding talkgroup %d: %s", packet.Dst, err)
				return
			}
		case repeaterExists:
			isToRepeater = true
			destRepeater, err = models.FindRepeaterByID(c.db, packet.Dst)
			if err != nil {
				klog.Errorf("Error finding repeater %d: %s", packet.Dst, err)
				return
			}
		default:
			klog.Errorf("Cannot find packet destination %d", packet.Dst)
			return
		}
	} else {
		// Find the user
		userExists, err = models.UserIDExists(c.db, packet.Dst)
		if err != nil {
			klog.Errorf("Error checking if user %d exists: %s", packet.Dst, err)
			return
		}

		if !userExists {
			klog.Errorf("Cannot find packet destination %d", packet.Dst)
			return
		}

		isToUser = true
		destUser, err = models.FindUserByID(c.db, packet.Dst)
		if err != nil {
			klog.Errorf("Error finding user %d: %s", packet.Dst, err)
			return
		}
	}

	logging.GetLogger(logging.Error).Logf(c.StartCall, "Starting call from %d to %d", packet.Src, packet.Dst)

	call := models.Call{
		StreamID:       packet.StreamID,
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
		LastFrameNum:   dmrconst.VoiceA,
		LastSeq:        256, //nolint:golint,gomnd // 256 is 1+ the max sequence number
		RSSI:           0,
		BER:            0.0,
		TotalBits:      0,
		HasHeader:      false,
		HasTerm:        false,
	}

	call.IsToRepeater = isToRepeater
	call.IsToUser = isToUser
	call.IsToTalkgroup = isToTalkgroup
	switch {
	case isToRepeater:
		call.ToRepeater = destRepeater
	case isToUser:
		call.ToUser = destUser
	case isToTalkgroup:
		call.ToTalkgroup = destTalkgroup
	}

	// Create the call in the database
	err = c.db.Create(&call).Error
	if err != nil {
		klog.Errorf("Error creating call: %v", err)
		return
	}

	// Add the call to the active calls map
	c.inFlightCallsMutex.Lock()
	c.inFlightCalls[call.ID] = &call
	c.inFlightCallsMutex.Unlock()

	if config.GetConfig().Debug {
		logging.GetLogger(logging.Access).Logf(c.StartCall, "Started call %d", call.StreamID)
	}

	// Add a timer that will end the call if we haven't seen a packet in 1 second.
	c.callEndTimersMutex.Lock()
	c.callEndTimers[call.ID] = time.AfterFunc(timerDelay, endCallHandler(ctx, c, packet))
	c.callEndTimersMutex.Unlock()
}

// IsCallActive checks if a call is active.
func (c *CallTracker) IsCallActive(ctx context.Context, packet models.Packet) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.IsCallActive")
	defer span.End()

	c.inFlightCallsMutex.RLock()
	for _, call := range c.inFlightCalls {
		if call.Active && call.StreamID == packet.StreamID && call.UserID == packet.Src && call.DestinationID == packet.Dst && call.TimeSlot == packet.Slot && call.GroupCall == packet.GroupCall {
			c.inFlightCallsMutex.RUnlock()
			return true
		}
	}
	c.inFlightCallsMutex.RUnlock()
	return false
}

func (c *CallTracker) publishCall(ctx context.Context, call *models.Call) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.publishCall")
	defer span.End()

	if (call.IsToRepeater || call.IsToTalkgroup) && call.GroupCall {
		// copy call into a jsonCallResponse
		var jsonCall apimodels.WSCallResponse
		jsonCall.ID = call.ID
		jsonCall.User.ID = call.User.ID
		jsonCall.User.Callsign = call.User.Callsign
		jsonCall.StartTime = call.StartTime
		jsonCall.Duration = call.Duration
		jsonCall.Active = call.Active
		jsonCall.TimeSlot = call.TimeSlot
		jsonCall.GroupCall = call.GroupCall
		if call.IsToTalkgroup {
			jsonCall.ToTalkgroup.ID = call.ToTalkgroup.ID
			jsonCall.ToTalkgroup.Name = call.ToTalkgroup.Name
			jsonCall.ToTalkgroup.Description = call.ToTalkgroup.Description
		}
		if call.IsToUser {
			jsonCall.ToUser.ID = call.ToUser.ID
			jsonCall.ToUser.Callsign = call.ToUser.Callsign
		}
		if call.IsToRepeater {
			jsonCall.ToRepeater.RadioID = call.ToRepeater.RadioID
			jsonCall.ToRepeater.Callsign = call.ToRepeater.Callsign
		}
		jsonCall.IsToTalkgroup = call.IsToTalkgroup
		jsonCall.IsToUser = call.IsToUser
		jsonCall.IsToRepeater = call.IsToRepeater
		jsonCall.Loss = call.Loss
		jsonCall.Jitter = call.Jitter
		jsonCall.BER = call.BER
		jsonCall.RSSI = call.RSSI
		// Publish the call JSON to Redis
		callJSON, err := json.Marshal(jsonCall)
		if err != nil {
			klog.Errorf("Error marshalling call JSON: %v", err)
			return
		}

		_, err = c.redis.Publish(ctx, "calls:public", callJSON).Result()
		if err != nil {
			klog.Errorf("Error publishing call JSON: %v", err)
			return
		}
	}

	origCallJSON, err := json.Marshal(call)
	if err != nil {
		klog.Errorf("Error marshalling call JSON: %v", err)
		return
	}
	_, err = c.redis.Publish(ctx, "calls", origCallJSON).Result()
	if err != nil {
		klog.Errorf("Error publishing call JSON: %v", err)
		return
	}
}

func (c *CallTracker) updateCall(ctx context.Context, call *models.Call, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.updateCall")
	defer span.End()

	// Reset call end timer
	c.callEndTimersMutex.Lock()
	c.callEndTimers[call.ID].Reset(timerDelay)
	c.callEndTimersMutex.Unlock()

	if call.LastSeq == packet.Seq {
		// This is a dup
		return
	}

	call.LastSeq = packet.Seq

	elapsed := time.Since(call.LastPacketTime)
	call.LastPacketTime = time.Now()
	// call.Jitter is a float32 that represents how many ms off from 60ms elapsed
	// time the last packet was. We'll use this to calculate the average jitter.
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-packetTimingMs)) / 2 //nolint:golint,gomnd

	call.Duration = time.Since(call.StartTime)

	lastTotalPackets := call.TotalPackets
	lastLostSequences := call.LostSequences
	calcSequenceLoss(call, packet)

	lastLoss := call.Loss
	call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)

	// Safety net: If the loss underflows, set it to the last value
	if call.Loss > 1 {
		call.Loss = lastLoss
		call.TotalPackets = lastTotalPackets
		call.LostSequences = lastLostSequences
	}

	call.TotalBits += 141
	if packet.BER > 0 {
		call.TotalErrors += packet.BER
	}

	call.BER = float32(call.TotalErrors) / float32(call.TotalBits)

	call.Active = true
	if packet.RSSI > 0 {
		call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2 //nolint:golint,gomnd
	}

	call.CallData = append(call.CallData, packet.DMRData[:]...)

	go c.publishCall(ctx, call)
}

func calcSequenceLoss(call *models.Call, packet models.Packet) {
	// Here we check the sequence number of the packet
	// If the sequence number is not what we expect, we increment the lost counter
	// We also increment the total counter

	// If the packet is a voice header, we reset the sequence number
	switch packet.FrameType {
	case dmrconst.FrameDataSync:
		// This is either a voice header or a voice terminator
		switch packet.DTypeOrVSeq {
		case uint(dmrconst.DTypeVoiceHead):
			// Voice header, this is the start of a voice superframe
			call.HasHeader = true
			call.TotalPackets++
			call.LastFrameNum = 0
		case uint(dmrconst.DTypeVoiceTerm):
			// Voice terminator
			if call.LastFrameNum != dmrconst.VoiceF {
				// We lost some number of packets
				call.LostSequences += dmrconst.VoiceF - call.LastFrameNum
				call.TotalPackets += dmrconst.VoiceF - call.LastFrameNum
			}
			call.TotalPackets++
			call.LastFrameNum = 0
		}
	case dmrconst.FrameVoiceSync:
		// This is a voice sync
		if !call.HasHeader && call.LastFrameNum == 0 {
			// We lost the header
			call.LostSequences++
			call.TotalPackets++
			call.HasHeader = true
		}
		// The previous packet should be either the header or the VoiceF frame
		// If it is the header, the sequence number should be 0
		// If it is the VoiceF frame, the sequence number should be VoiceF
		// If it is anything else, we lost some number of packets
		if call.LastFrameNum != 0 && call.LastFrameNum != dmrconst.VoiceF {
			call.LostSequences += packet.DTypeOrVSeq - call.LastFrameNum - 1
			call.TotalPackets += packet.DTypeOrVSeq - call.LastFrameNum - 1
		}
		call.TotalPackets++
		call.LastFrameNum = packet.DTypeOrVSeq
	case dmrconst.FrameVoice:
		// This is a voice packet
		if !call.HasHeader {
			// We lost the header and any sequences between the header and this packet
			call.LostSequences += 1 + packet.DTypeOrVSeq
			call.TotalPackets += 1 + packet.DTypeOrVSeq
			call.HasHeader = true
		} else if int(packet.DTypeOrVSeq) != int(call.LastFrameNum)+1 {
			// We lost some number of packets
			// We need to be careful here as the sequence number can wrap around and cause us to get a negative number
			if int(packet.DTypeOrVSeq) < int(call.LastFrameNum) {
				// We wrapped around.
				call.LostSequences += dmrconst.VoiceF - call.LastFrameNum + packet.DTypeOrVSeq
				call.TotalPackets += dmrconst.VoiceF - call.LastFrameNum + packet.DTypeOrVSeq
			} else {
				call.LostSequences += packet.DTypeOrVSeq - call.LastFrameNum - 1
				call.TotalPackets += packet.DTypeOrVSeq - call.LastFrameNum - 1
			}
		}
		// We got the packet we expected
		call.TotalPackets++
		call.LastFrameNum = packet.DTypeOrVSeq
	}
}

// ProcessCallPacket processes a packet and updates the call.
func (c *CallTracker) ProcessCallPacket(ctx context.Context, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.ProcessCallPacket")
	defer span.End()

	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	c.inFlightCallsMutex.Lock()
	for _, lcall := range c.inFlightCalls {
		if lcall.StreamID == packet.StreamID && lcall.Active && lcall.TimeSlot == packet.Slot && lcall.GroupCall == packet.GroupCall && lcall.UserID == packet.Src {
			c.updateCall(ctx, lcall, packet)
			break
		}
	}
	c.inFlightCallsMutex.Unlock()
}

func endCallHandler(ctx context.Context, c *CallTracker, packet models.Packet) func() {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "calltracker.endCallHandler")
	defer span.End()

	return func() {
		klog.Errorf("Call %d timed out", packet.StreamID)
		c.EndCall(ctx, packet)
	}
}

// EndCall ends a call.
func (c *CallTracker) EndCall(ctx context.Context, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.EndCall")
	defer span.End()

	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	c.inFlightCallsMutex.Lock()
	for _, call := range c.inFlightCalls {
		if call.StreamID == packet.StreamID && call.Active && call.TimeSlot == packet.Slot && call.GroupCall == packet.GroupCall && call.UserID == packet.Src {
			// Delete the call end timer
			timer := c.callEndTimers[call.ID]
			timer.Stop()
			delete(c.callEndTimers, call.ID)

			if time.Since(call.StartTime) < 100*time.Millisecond {
				// This is probably a key-up, so delete the call from the db
				call := call
				c.db.Delete(&call)
				break
			}

			call.Duration = time.Since(call.StartTime)
			call.Active = false

			err := c.db.Save(call).Error
			if err != nil {
				klog.Errorf("Error saving call: %v", err)
				break
			}

			c.publishCall(ctx, call)
			delete(c.inFlightCalls, call.ID)

			logging.GetLogger(logging.Access).Logf(c.EndCall, "Call %d from %d to %d via %d ended with duration %v, %f%% Loss, %f%% BER, %fdBm RSSI, and %fms Jitter", packet.StreamID, packet.Src, packet.Dst, packet.Repeater, call.Duration, call.Loss*pct, call.BER*pct, call.RSSI, call.Jitter)
		}
	}
	c.inFlightCallsMutex.Unlock()
}
