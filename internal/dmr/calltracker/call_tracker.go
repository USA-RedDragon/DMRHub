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
	"errors"
	"fmt"
	"log/slog"
	"sync"
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

// netEventPayload mirrors the fields we need from WSNetEventResponse
// for decoding pubsub messages without importing the apimodels cycle.
type netEventPayload struct {
	NetID       uint   `json:"net_id"`
	TalkgroupID uint   `json:"talkgroup_id"`
	Event       string `json:"event"`
}

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
		Active:        true, // Always true: calls are only hashed while active
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
	inFlightCalls *xsync.Map[uint64, *inFlightCall]
	// activeNets maps talkgroup ID → net ID for currently active nets.
	// Updated via pubsub subscription to avoid a DB query per call end.
	activeNets *xsync.Map[uint, uint]
	cancelNets context.CancelFunc
}

// inFlightCall wraps a models.Call with a mutex to prevent data races between
// the packet-processing goroutine (updateCall) and timer-fired goroutine (EndCall).
type inFlightCall struct {
	mu    sync.Mutex
	call  *models.Call
	ended bool
}

// NewCallTracker creates a new CallTracker.
func NewCallTracker(db *gorm.DB, pubsub pubsub.PubSub) *CallTracker {
	ct := &CallTracker{
		db:            db,
		pubsub:        pubsub,
		callEndTimers: xsync.NewMap[uint64, *time.Timer](),
		inFlightCalls: xsync.NewMap[uint64, *inFlightCall](),
		activeNets:    xsync.NewMap[uint, uint](),
	}
	ct.loadActiveNets()
	ct.subscribeNetEvents()
	return ct
}

// Stop cancels the net-events subscription goroutine.
func (c *CallTracker) Stop() {
	if c.cancelNets != nil {
		c.cancelNets()
	}
}

// loadActiveNets populates the activeNets cache from the database at startup.
func (c *CallTracker) loadActiveNets() {
	var nets []models.Net
	if err := c.db.Where("active = ?", true).Find(&nets).Error; err != nil {
		slog.Error("CallTracker: failed to load active nets", "error", err)
		return
	}
	for i := range nets {
		c.activeNets.Store(nets[i].TalkgroupID, nets[i].ID)
	}
	if len(nets) > 0 {
		slog.Info("CallTracker: loaded active nets into cache", "count", len(nets))
	}
}

// subscribeNetEvents subscribes to net:events pubsub and keeps activeNets in sync.
func (c *CallTracker) subscribeNetEvents() {
	sub := c.pubsub.Subscribe("net:events")
	ctx, cancel := context.WithCancel(context.Background())
	c.cancelNets = cancel
	go func() {
		ch := sub.Channel()
		for {
			select {
			case <-ctx.Done():
				if err := sub.Close(); err != nil {
					slog.Error("CallTracker: failed to close net events subscription", "error", err)
				}
				return
			case msg, ok := <-ch:
				if !ok {
					// Channel closed (pubsub shutdown) — exit silently.
					return
				}
				if len(msg) == 0 {
					continue
				}
				var evt netEventPayload
				if err := json.Unmarshal(msg, &evt); err != nil {
					slog.Error("CallTracker: failed to unmarshal net event", "error", err)
					continue
				}
				switch evt.Event {
				case "started":
					c.activeNets.Store(evt.TalkgroupID, evt.NetID)
				case "stopped":
					c.activeNets.Delete(evt.TalkgroupID)
				}
			}
		}
	}()
}

// callDestination holds the resolved destination for a call.
type callDestination struct {
	isToRepeater  bool
	isToTalkgroup bool
	isToUser      bool
	talkgroup     models.Talkgroup
	repeater      models.Repeater
	user          models.User
}

// lookupSourceUser checks that the source user exists and returns it.
func (c *CallTracker) lookupSourceUser(srcID uint) (models.User, error) {
	user, err := models.FindUserByID(c.db, srcID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.User{}, fmt.Errorf("user %d does not exist", srcID)
		}
		return models.User{}, fmt.Errorf("error finding user %d: %w", srcID, err)
	}
	return user, nil
}

// lookupSourceRepeater checks that the source repeater exists and returns it.
func (c *CallTracker) lookupSourceRepeater(repeaterID uint) (models.Repeater, error) {
	repeater, err := models.FindRepeaterByID(c.db, repeaterID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return models.Repeater{}, fmt.Errorf("repeater %d does not exist", repeaterID)
		}
		return models.Repeater{}, fmt.Errorf("error finding repeater %d: %w", repeaterID, err)
	}
	return repeater, nil
}

// resolveDestination determines the target of a call (talkgroup, repeater, or user).
func (c *CallTracker) resolveDestination(packet models.Packet) (*callDestination, error) {
	dest := &callDestination{}

	if packet.GroupCall {
		return c.resolveGroupCallDestination(packet.Dst, dest)
	}

	// Private call — destination is a user.
	user, err := models.FindUserByID(c.db, packet.Dst)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("cannot find packet destination %d", packet.Dst)
		}
		return nil, fmt.Errorf("error finding destination user %d: %w", packet.Dst, err)
	}
	dest.isToUser = true
	dest.user = user
	return dest, nil
}

// resolveGroupCallDestination resolves the destination for a group call (talkgroup or repeater).
func (c *CallTracker) resolveGroupCallDestination(dstID uint, dest *callDestination) (*callDestination, error) {
	// Try talkgroup first (most common case for group calls).
	talkgroup, err := models.FindTalkgroupByID(c.db, dstID)
	if err == nil {
		dest.isToTalkgroup = true
		dest.talkgroup = talkgroup
		return dest, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("error finding talkgroup %d: %w", dstID, err)
	}

	// Fall back to repeater.
	repeater, err := models.FindRepeaterByID(c.db, dstID)
	if err == nil {
		dest.isToRepeater = true
		dest.repeater = repeater
		return dest, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, fmt.Errorf("error finding repeater %d: %w", dstID, err)
	}

	return nil, fmt.Errorf("cannot find packet destination %d", dstID)
}

// newCallFromPacket builds a models.Call from the packet, source entities, and resolved destination.
func newCallFromPacket(packet models.Packet, sourceUser models.User, sourceRepeater models.Repeater, dest *callDestination) models.Call {
	now := time.Now()
	call := models.Call{
		StreamID:       packet.StreamID,
		StartTime:      now,
		Active:         true,
		UserID:         sourceUser.ID,
		RepeaterID:     sourceRepeater.ID,
		TimeSlot:       packet.Slot,
		GroupCall:      packet.GroupCall,
		DestinationID:  packet.Dst,
		TotalPackets:   0,
		LostSequences:  0,
		LastPacketTime: now,
		Loss:           0.0,
		Jitter:         0.0,
		LastFrameNum:   dmrconst.VoiceA,
		LastSeq:        256, // 256 is 1+ the max sequence number
		RSSI:           0,
		BER:            0.0,
		TotalBits:      0,
		HasHeader:      false,
		HasTerm:        false,
		IsToRepeater:   dest.isToRepeater,
		IsToUser:       dest.isToUser,
		IsToTalkgroup:  dest.isToTalkgroup,
	}

	switch {
	case dest.isToRepeater:
		call.ToRepeaterID = &dest.repeater.ID
	case dest.isToUser:
		call.ToUserID = &dest.user.ID
	case dest.isToTalkgroup:
		call.ToTalkgroupID = &dest.talkgroup.ID
	}

	return call
}

// setCallAssociations populates the in-memory association fields on a call
// (used for logging, publishing, etc.) without persisting back to the DB.
func setCallAssociations(call *models.Call, sourceUser models.User, sourceRepeater models.Repeater, dest *callDestination) {
	call.User = sourceUser
	call.Repeater = sourceRepeater
	switch {
	case dest.isToRepeater:
		call.ToRepeater = dest.repeater
	case dest.isToUser:
		call.ToUser = dest.user
	case dest.isToTalkgroup:
		call.ToTalkgroup = dest.talkgroup
	}
}

// persistAndTrackCall saves the call to the database, stores it in the
// in-flight map, and starts the call-end timer.
func (c *CallTracker) persistAndTrackCall(_ context.Context, call *models.Call, packet models.Packet) error {
	// Create the call in the database.
	// Omit associations to prevent GORM from cascade-saving related records.
	// Omit CallData — it's always empty at creation time.
	if err := c.db.Omit(clause.Associations, "CallData").Create(call).Error; err != nil {
		return fmt.Errorf("error creating call: %w", err)
	}

	callHash, err := getCallHash(*call)
	if err != nil {
		return err
	}

	// Add the call to the active calls map
	c.inFlightCalls.Store(callHash, &inFlightCall{call: call})

	// Add a timer that will end the call if we haven't seen a packet in 2 seconds.
	c.callEndTimers.Store(callHash, time.AfterFunc(timerDelay, endCallHandler(c, packet))) //nolint:contextcheck // Timer fires after request context is done; intentionally uses background context

	return nil
}

// StartCall starts tracking a new call.
func (c *CallTracker) StartCall(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.StartCall")
	defer span.End()

	sourceUser, err := c.lookupSourceUser(packet.Src)
	if err != nil {
		slog.Error("StartCall: source user lookup failed", "error", err)
		return
	}

	sourceRepeater, err := c.lookupSourceRepeater(packet.Repeater)
	if err != nil {
		slog.Error("StartCall: source repeater lookup failed", "error", err)
		return
	}

	dest, err := c.resolveDestination(packet)
	if err != nil {
		slog.Error("StartCall: destination resolution failed", "error", err)
		return
	}

	slog.Debug("Starting call", "src", packet.Src, "dst", packet.Dst)

	call := newCallFromPacket(packet, sourceUser, sourceRepeater, dest)

	if err := c.persistAndTrackCall(ctx, &call, packet); err != nil {
		slog.Error("StartCall: persist failed", "error", err)
		return
	}

	setCallAssociations(&call, sourceUser, sourceRepeater, dest)

	slog.Debug("Started call", "streamID", call.StreamID, "src", call.User.Callsign, "dst", call.DestinationID, "repeater", call.Repeater.Callsign)
}

// IsCallActive checks if a call is active.
func (c *CallTracker) IsCallActive(ctx context.Context, packet models.Packet) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.IsCallActive")
	defer span.End()

	callHash, err := getCallHashFromPacket(packet)
	if err != nil {
		return false
	}

	ifc, ok := c.inFlightCalls.Load(callHash)
	if !ok {
		return false
	}
	ifc.mu.Lock()
	defer ifc.mu.Unlock()
	return !ifc.ended
}

func (c *CallTracker) publishCall(ctx context.Context, call *models.Call) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.publishCall")
	defer span.End()

	if (call.IsToRepeater || call.IsToTalkgroup) && call.GroupCall {
		jsonCall := apimodels.NewWSCallResponseFromCall(call)
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

func (c *CallTracker) updateCall(ctx context.Context, ifc *inFlightCall, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "CallTracker.updateCall")
	defer span.End()

	ifc.mu.Lock()
	if ifc.ended {
		ifc.mu.Unlock()
		return
	}
	call := ifc.call

	hash, err := getCallHash(*call)
	if err != nil {
		ifc.mu.Unlock()
		return
	}

	timer, ok := c.callEndTimers.Load(hash)
	if !ok {
		ifc.mu.Unlock()
		return
	}

	// Reset call end timer
	timer.Reset(timerDelay)

	if call.LastSeq == packet.Seq {
		// This is a dup
		ifc.mu.Unlock()
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

	// Snapshot the call to avoid a data race between the publish goroutine
	// (which reads fields via json.Marshal) and EndCall (which writes fields).
	callCopy := *call
	ifc.mu.Unlock()
	go c.publishCall(ctx, &callCopy)
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

	ifc, ok := c.inFlightCalls.Load(hash)
	if !ok {
		slog.Error("Active call not found")
		return
	}

	c.updateCall(ctx, ifc, packet)
}

func endCallHandler(c *CallTracker, packet models.Packet) func() {
	return func() {
		// Use a fresh background context because the original request context
		// from StartCall is canceled by the time the timer fires.
		ctx := context.Background()
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

	ifc, ok := c.inFlightCalls.LoadAndDelete(hash)
	if !ok {
		slog.Error("Active call not found")
		return
	}

	ifc.mu.Lock()
	if ifc.ended {
		ifc.mu.Unlock()
		return
	}
	ifc.ended = true
	call := ifc.call

	if time.Since(call.StartTime) < 100*time.Millisecond {
		ifc.mu.Unlock()
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
	ifc.mu.Unlock()

	err = c.db.Omit(clause.Associations).Save(call).Error
	if err != nil {
		slog.Error("Error saving call", "error", err)
		return
	}

	c.publishCall(ctx, call)

	// If this call's talkgroup has an active net, publish a check-in event.
	c.publishNetCheckIn(call)

	slog.Info("Call ended", "streamID", packet.StreamID, "src", packet.Src, "dst", packet.Dst, "repeater", packet.Repeater, "duration", call.Duration, "loss", call.Loss*pct, "ber", call.BER*pct, "rssi", call.RSSI, "jitter", call.Jitter)
}

// publishNetCheckIn publishes a check-in event if the call's talkgroup has an active net.
func (c *CallTracker) publishNetCheckIn(call *models.Call) {
	if !call.GroupCall || call.ToTalkgroupID == nil {
		return
	}

	netID, ok := c.activeNets.Load(*call.ToTalkgroupID)
	if !ok {
		return
	}

	checkIn := apimodels.WSNetCheckInResponse{
		NetID:  netID,
		CallID: call.ID,
		User: apimodels.WSCallResponseUser{
			ID:       call.User.ID,
			Callsign: call.User.Callsign,
		},
		StartTime: call.StartTime,
		Duration:  call.Duration,
	}

	data, err := json.Marshal(checkIn)
	if err != nil {
		slog.Error("CallTracker: failed to marshal net check-in", "error", err)
		return
	}

	topic := fmt.Sprintf("net:checkins:%d", netID)
	if err := c.pubsub.Publish(topic, data); err != nil {
		slog.Error("CallTracker: failed to publish net check-in", "error", err)
	}
}
