// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2026 Jacob McSwain
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
	"fmt"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/mitchellh/hashstructure/v2"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// Assuming +/-7ms of jitter, we'll wait 2 seconds before we consider a call to be over
// This equates out to about 30 lost voice packets.
const timerDelay = 2 * time.Second
const packetTimingMs = 60
const pct = 100

// These are the keys that we use to create a consistent hash
type callMapStruct struct {
	Active        bool
	StreamID      uint
	UserID        uint
	DestinationID uint
	TimeSlot      bool
	GroupCall     bool
}

func getCallHashFromPacket(packet models.Packet) (uint64, error) {
	v := callMapStruct{
		Active:        true,
		StreamID:      packet.StreamID,
		UserID:        packet.Src,
		DestinationID: packet.Dst,
		TimeSlot:      packet.Slot,
		GroupCall:     packet.GroupCall,
	}

	hash, err := hashstructure.Hash(v, hashstructure.FormatV2, nil)
	if err != nil {
		slog.Error("CallTracker: Error hashing call", "error", err)
		return hash, fmt.Errorf("failed to hash call from packet: %w", err)
	}
	return hash, nil
}

func getCallHash(call models.Call) (uint64, error) {
	v := callMapStruct{
		Active:        call.Active,
		StreamID:      call.StreamID,
		UserID:        call.UserID,
		DestinationID: call.DestinationID,
		TimeSlot:      call.TimeSlot,
		GroupCall:     call.GroupCall,
	}

	hash, err := hashstructure.Hash(v, hashstructure.FormatV2, nil)
	if err != nil {
		slog.Error("CallTracker: Error hashing call", "error", err)
		return hash, fmt.Errorf("failed to hash call: %w", err)
	}
	return hash, nil
}

// CallTracker is a struct that holds the state of the calls that are currently in progress.
type CallTracker struct {
	db            *gorm.DB
	pubsub        pubsub.PubSub
	callEndTimers *xsync.Map[uint64, *time.Timer]
	inFlightCalls *xsync.Map[uint64, *models.Call]
}

// NewCallTracker creates a new CallTracker.
func NewCallTracker(db *gorm.DB, pubsub pubsub.PubSub) *CallTracker {
	return &CallTracker{
		db:            db,
		pubsub:        pubsub,
		callEndTimers: xsync.NewMap[uint64, *time.Timer](),
		inFlightCalls: xsync.NewMap[uint64, *models.Call](),
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
		slog.Error("Error checking if user exists", "userID", packet.Src, "error", err)
		return
	}

	if !userExists {
		slog.Debug("User does not exist", "userID", packet.Src)
		return
	}

	sourceUser, err = models.FindUserByID(c.db, packet.Src)
	if err != nil {
		slog.Error("Error finding user", "userID", packet.Src, "error", err)
		return
	}

	repeaterExists, err := models.RepeaterIDExists(c.db, packet.Repeater)
	if err != nil {
		slog.Error("Error checking if repeater exists", "repeaterID", packet.Repeater, "error", err)
		return
	}

	if !repeaterExists {
		slog.Debug("Repeater does not exist", "repeaterID", packet.Repeater)
		return
	}

	sourceRepeater, err = models.FindRepeaterByID(c.db, packet.Repeater)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", packet.Repeater, "error", err)
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
			slog.Error("Error checking if talkgroup exists", "talkgroupID", packet.Dst, "error", err)
			return
		}

		repeaterExists, err := models.RepeaterIDExists(c.db, packet.Dst)
		if err != nil {
			slog.Error("Error checking if repeater exists", "repeaterID", packet.Dst, "error", err)
			return
		}

		switch {
		case talkgroupExists:
			isToTalkgroup = true
			destTalkgroup, err = models.FindTalkgroupByID(c.db, packet.Dst)
			if err != nil {
				slog.Error("Error finding talkgroup", "talkgroupID", packet.Dst, "error", err)
				return
			}
		case repeaterExists:
			isToRepeater = true
			destRepeater, err = models.FindRepeaterByID(c.db, packet.Dst)
			if err != nil {
				slog.Error("Error finding repeater", "repeaterID", packet.Dst, "error", err)
				return
			}
		default:
			slog.Error("Cannot find packet destination", "destinationID", packet.Dst)
			return
		}
	} else {
		// Find the user
		userExists, err = models.UserIDExists(c.db, packet.Dst)
		if err != nil {
			slog.Error("Error checking if user exists", "userID", packet.Dst, "error", err)
			return
		}

		if !userExists {
			slog.Error("Cannot find packet destination", "destinationID", packet.Dst)
			return
		}

		isToUser = true
		destUser, err = models.FindUserByID(c.db, packet.Dst)
		if err != nil {
			slog.Error("Error finding user", "userID", packet.Dst, "error", err)
			return
		}
	}

	slog.Debug("Starting call", "src", packet.Src, "dst", packet.Dst)

	call := models.Call{
		StreamID:       packet.StreamID,
		StartTime:      time.Now(),
		Active:         true,
		UserID:         sourceUser.ID,
		RepeaterID:     sourceRepeater.ID,
		TimeSlot:       packet.Slot,
		GroupCall:      packet.GroupCall,
		DestinationID:  packet.Dst,
		TotalPackets:   0,
		LostSequences:  0,
		LastPacketTime: time.Now(),
		Loss:           0.0,
		Jitter:         0.0,
		LastFrameNum:   dmrconst.VoiceA,
		LastSeq:        256, // 256 is 1+ the max sequence number
		RSSI:           0,
		BER:            0.0,
		TotalBits:      0,
		HasHeader:      false,
		HasTerm:        false,
		IsToRepeater:   isToRepeater,
		IsToUser:       isToUser,
		IsToTalkgroup:  isToTalkgroup,
	}

	switch {
	case isToRepeater:
		call.ToRepeaterID = &destRepeater.ID
	case isToUser:
		call.ToUserID = &destUser.ID
	case isToTalkgroup:
		call.ToTalkgroupID = &destTalkgroup.ID
	}

	// Create the call in the database.
	// Omit associations to prevent GORM from cascade-saving related records.
	// Omit CallData — it's always empty at creation time.
	err = c.db.Omit(clause.Associations, "CallData").Create(&call).Error
	if err != nil {
		slog.Error("Error creating call", "error", err)
		return
	}

	// Set association objects for in-memory use (logging, publishing, etc.)
	call.User = sourceUser
	call.Repeater = sourceRepeater
	switch {
	case isToRepeater:
		call.ToRepeater = destRepeater
	case isToUser:
		call.ToUser = destUser
	case isToTalkgroup:
		call.ToTalkgroup = destTalkgroup
	}

	callHash, err := getCallHash(call)
	if err != nil {
		return
	}

	// Add the call to the active calls map
	c.inFlightCalls.Store(callHash, &call)

	slog.Debug("Started call", "streamID", call.StreamID, "src", call.User.Callsign, "dst", call.DestinationID, "repeater", call.Repeater.Callsign)

	// Add a timer that will end the call if we haven't seen a packet in 1 second.
	c.callEndTimers.Store(callHash, time.AfterFunc(timerDelay, endCallHandler(ctx, c, packet)))
}

// IsCallActive checks if a call is active.
func (c *CallTracker) IsCallActive(ctx context.Context, packet models.Packet) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.IsCallActive")
	defer span.End()

	callHash, err := getCallHashFromPacket(packet)
	if err != nil {
		return false
	}

	_, ok := c.inFlightCalls.Load(callHash)
	return ok
}

func (c *CallTracker) publishCall(ctx context.Context, call *models.Call) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.publishCall")
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
			jsonCall.ToRepeater.RadioID = call.ToRepeater.ID
			jsonCall.ToRepeater.Callsign = call.ToRepeater.Callsign
		}
		jsonCall.IsToTalkgroup = call.IsToTalkgroup
		jsonCall.IsToUser = call.IsToUser
		jsonCall.IsToRepeater = call.IsToRepeater
		jsonCall.Loss = call.Loss
		jsonCall.Jitter = call.Jitter
		jsonCall.BER = call.BER
		jsonCall.RSSI = call.RSSI
		// Publish the call JSON to pubsub
		callJSON, err := json.Marshal(jsonCall)
		if err != nil {
			slog.Error("Error marshalling call JSON", "error", err)
			return
		}

		if err = c.pubsub.Publish("calls:public", callJSON); err != nil {
			slog.Error("Error publishing call JSON", "error", err)
			return
		}
	}

	origCallJSON, err := json.Marshal(call)
	if err != nil {
		slog.Error("Error marshalling call JSON", "error", err)
		return
	}
	if err = c.pubsub.Publish("calls", origCallJSON); err != nil {
		slog.Error("Error publishing call JSON", "error", err)
		return
	}
}

func (c *CallTracker) updateCall(ctx context.Context, call *models.Call, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.updateCall")
	defer span.End()

	hash, err := getCallHash(*call)
	if err != nil {
		return
	}

	timer, ok := c.callEndTimers.Load(hash)
	if !ok {
		return
	}

	// Reset call end timer
	timer.Reset(timerDelay)

	if call.LastSeq == packet.Seq {
		// This is a dup
		return
	}

	call.LastSeq = packet.Seq

	elapsed := time.Since(call.LastPacketTime)
	call.LastPacketTime = time.Now()
	// call.Jitter is a float32 that represents how many ms off from 60ms elapsed
	// time the last packet was. We'll use this to calculate the average jitter.
	call.Jitter = (call.Jitter + float32(elapsed.Milliseconds()-packetTimingMs)) / 2

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
		call.RSSI = (call.RSSI + float32(packet.RSSI)) / 2
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
		} else if packet.DTypeOrVSeq != call.LastFrameNum+1 {
			// We lost some number of packets
			// We need to be careful here as the sequence number can wrap around and cause us to get a negative number
			if packet.DTypeOrVSeq < call.LastFrameNum {
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

	hash, err := getCallHashFromPacket(packet)
	if err != nil {
		slog.Error("Error getting call hash from packet", "error", err)
		return
	}

	call, ok := c.inFlightCalls.Load(hash)
	if !ok {
		slog.Error("Active call not found")
		return
	}

	c.updateCall(ctx, call, packet)
}

func endCallHandler(ctx context.Context, c *CallTracker, packet models.Packet) func() {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "calltracker.endCallHandler")
	defer span.End()

	return func() {
		slog.Error("Call timed out", "streamID", packet.StreamID)
		c.EndCall(ctx, packet)
	}
}

// EndCall ends a call.
func (c *CallTracker) EndCall(ctx context.Context, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.EndCall")
	defer span.End()

	hash, err := getCallHashFromPacket(packet)
	if err != nil {
		slog.Error("Error getting call hash from packet", "error", err)
		return
	}

	call, ok := c.inFlightCalls.LoadAndDelete(hash)
	if !ok {
		slog.Error("Active call not found")
		return
	}

	if time.Since(call.StartTime) < 100*time.Millisecond {
		// This is probably a key-up, so delete the call from the db
		c.db.Unscoped().Delete(call)
		return
	}

	// Delete the call end timer
	timer, ok := c.callEndTimers.LoadAndDelete(hash)
	if !ok {
		slog.Error("Call end timer not found")
	} else {
		timer.Stop()
	}

	call.Duration = time.Since(call.StartTime)
	call.Active = false

	err = c.db.Omit(clause.Associations).Save(call).Error
	if err != nil {
		slog.Error("Error saving call", "error", err)
		return
	}

	c.publishCall(ctx, call)

	slog.Info("Call ended", "streamID", packet.StreamID, "src", packet.Src, "dst", packet.Dst, "repeater", packet.Repeater, "duration", call.Duration, "loss", call.Loss*pct, "ber", call.BER*pct, "rssi", call.RSSI, "jitter", call.Jitter)
}
