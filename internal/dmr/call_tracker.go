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

package dmr

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/redis/go-redis/v9"
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
	var sourceUser models.User
	var sourceRepeater models.Repeater

	if !models.UserIDExists(c.db, packet.Src) {
		if config.GetConfig().Debug {
			klog.Errorf("User %d does not exist", packet.Src)
		}
		return
	}
	sourceUser = models.FindUserByID(c.db, packet.Src)

	if !models.RepeaterIDExists(c.db, packet.Repeater) {
		klog.Errorf("Repeater %d does not exist", packet.Repeater)
		return
	}
	sourceRepeater = models.FindRepeaterByID(c.db, packet.Repeater)

	isToRepeater, isToTalkgroup, isToUser := false, false, false
	var destUser models.User
	var destRepeater models.Repeater
	var destTalkgroup models.Talkgroup

	// Try the different targets for the given call destination
	// if packet.GroupCall is true, then packet.Dst is either a talkgroup or a repeater
	// if packet.GroupCall is false, then packet.Dst is a user
	if packet.GroupCall {
		// Decide between talkgroup and repeater
		if !models.TalkgroupIDExists(c.db, packet.Dst) {
			if !models.RepeaterIDExists(c.db, packet.Dst) {
				klog.Errorf("Cannot find packet destination %d", packet.Dst)
				return
			}
			isToRepeater = true
			destRepeater = models.FindRepeaterByID(c.db, packet.Dst)
		} else {
			isToTalkgroup = true
			destTalkgroup = models.FindTalkgroupByID(c.db, packet.Dst)
		}
	} else {
		// Find the user
		if !models.UserIDExists(c.db, packet.Dst) {
			klog.Errorf("Cannot find packet destination %d", packet.Dst)
			return
		}
		isToUser = true
		destUser = models.FindUserByID(c.db, packet.Dst)
	}

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
	c.db.Create(&call)
	if c.db.Error != nil {
		klog.Errorf("Error creating call: %v", c.db.Error)
		return
	}

	// Add the call to the active calls map
	c.inFlightCallsMutex.Lock()
	c.inFlightCalls[call.ID] = &call
	c.inFlightCallsMutex.Unlock()

	if config.GetConfig().Debug {
		klog.Infof("Started call %d", call.StreamID)
	}

	// Add a timer that will end the call if we haven't seen a packet in 1 second.
	c.callEndTimersMutex.Lock()
	c.callEndTimers[call.ID] = time.AfterFunc(timerDelay, endCallHandler(ctx, c, packet))
	c.callEndTimersMutex.Unlock()
}

// IsCallActive checks if a call is active.
func (c *CallTracker) IsCallActive(packet models.Packet) bool {
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
		_, err = c.redis.Publish(ctx, "calls", callJSON).Result()
		if err != nil {
			klog.Errorf("Error publishing call JSON: %v", err)
			return
		}
	}

	// This definitely needs to be refactored to not loop
	// through all repeaters every time a call packet is received
	go func() {
		// Iterate all repeaters to see if they want the call
		var repeaters []models.Repeater
		alreadyPublished := make(map[uint]bool)
		c.db.Preload("Owner").Find(&repeaters)
		for _, repeater := range repeaters {
			want, _ := repeater.WantRX(packet)
			if want && !alreadyPublished[repeater.OwnerID] {
				// Publish the call to the repeater owner's call history
				c.redis.Publish(ctx, fmt.Sprintf("calls:%d", repeater.OwnerID), callJSON)
				alreadyPublished[repeater.OwnerID] = true
				continue
			}
			if repeater.OwnerID == call.UserID {
				c.redis.Publish(ctx, fmt.Sprintf("calls:%d", repeater.OwnerID), callJSON)
				alreadyPublished[repeater.OwnerID] = true
			}
		}
	}()
}

func (c *CallTracker) updateCall(ctx context.Context, call *models.Call, packet models.Packet) {
	// Reset call end timer
	c.callEndTimersMutex.Lock()
	c.callEndTimers[call.ID].Reset(timerDelay)
	c.callEndTimersMutex.Unlock()

	elapsed := time.Since(call.LastPacketTime)
	call.LastPacketTime = time.Now()
	// call.Jitter is a float32 that represents how many ms off from 60ms elapsed
	// time the last packet was. We'll use this to calculate the average jitter.
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-packetTimingMs)) / 2 //nolint:golint,gomnd

	calcSequenceLoss(call, packet)

	if packet.BER > 0 {
		call.TotalBits += 141
		call.BER = ((call.BER + float32(packet.BER)) / float32(call.TotalBits)) / 2 //nolint:golint,gomnd
	}

	call.Duration = time.Since(call.StartTime)
	call.Loss = float32(call.LostSequences) / float32(call.TotalPackets)
	call.Active = true
	if packet.RSSI > 0 {
		call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2 //nolint:golint,gomnd
	}

	if call.TotalPackets%2 == 0 {
		go c.publishCall(ctx, call, packet)
	}
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
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	c.inFlightCallsMutex.RLock()
	for _, lcall := range c.inFlightCalls {
		c.inFlightCallsMutex.RUnlock()
		defer c.inFlightCallsMutex.RLock()
		if lcall.StreamID == packet.StreamID && lcall.Active && lcall.TimeSlot == packet.Slot && lcall.GroupCall == packet.GroupCall && lcall.UserID == packet.Src {
			c.updateCall(ctx, lcall, packet)
			return
		}
	}
	c.inFlightCallsMutex.RUnlock()
}

func endCallHandler(ctx context.Context, c *CallTracker, packet models.Packet) func() {
	return func() {
		klog.Errorf("Call %d timed out", packet.StreamID)
		c.EndCall(ctx, packet)
	}
}

// EndCall ends a call.
func (c *CallTracker) EndCall(ctx context.Context, packet models.Packet) {
	// Querying on packet.StreamId and call.Active should be enough to find the call, but in the event that there are multiple calls
	// active that somehow have the same StreamId, we'll also query on the other fields.
	c.inFlightCallsMutex.RLock()
	for _, call := range c.inFlightCalls {
		c.inFlightCallsMutex.RUnlock()
		defer c.inFlightCallsMutex.RLock()
		if call.StreamID == packet.StreamID && call.Active && call.TimeSlot == packet.Slot && call.GroupCall == packet.GroupCall && call.UserID == packet.Src {
			// Delete the call end timer
			c.callEndTimersMutex.RLock()
			timer := c.callEndTimers[call.ID]
			c.callEndTimersMutex.RUnlock()
			timer.Stop()
			c.callEndTimersMutex.Lock()
			delete(c.callEndTimers, call.ID)
			c.callEndTimersMutex.Unlock()

			if time.Since(call.StartTime) < 100*time.Millisecond {
				// This is probably a key-up, so delete the call from the db
				call := call
				c.db.Delete(&call)
				break
			}

			// If the call doesn't have a term, we lost that packet
			if !call.HasTerm {
				call.LostSequences++
				call.TotalPackets++
				if config.GetConfig().Debug {
					klog.Errorf("Call %d ended without a term", packet.StreamID)
				}
			}

			// If lastFrameNum != 5, Calculate the number of lost packets by subtracting the last frame number from 5 and adding it to the lost sequences
			if call.LastFrameNum != dmrconst.VoiceF {
				call.LostSequences += dmrconst.VoiceF - call.LastFrameNum
				call.TotalPackets += dmrconst.VoiceF - call.LastFrameNum
				if config.GetConfig().Debug {
					klog.Errorf("Call %d ended with %d lost packets", packet.StreamID, dmrconst.VoiceF-call.LastFrameNum)
				}
			}

			call.Active = false
			call.Duration = time.Since(call.StartTime)
			call.Loss = float32(call.LostSequences / call.TotalPackets)
			c.db.Save(call)
			c.publishCall(ctx, call, packet)
			c.inFlightCallsMutex.Lock()
			delete(c.inFlightCalls, call.ID)
			c.inFlightCallsMutex.Unlock()

			klog.Infof("Call %d from %d to %d via %d ended with duration %v, %f%% Loss, %f%% BER, %fdBm RSSI, and %fms Jitter", packet.StreamID, packet.Src, packet.Dst, packet.Repeater, call.Duration, call.Loss*pct, call.BER*pct, call.RSSI, call.Jitter)
		}
	}
	c.inFlightCallsMutex.RUnlock()
}
