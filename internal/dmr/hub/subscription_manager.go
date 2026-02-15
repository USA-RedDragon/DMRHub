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

package hub

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
)

// subscriptionManager tracks repeater-to-talkgroup subscriptions and manages
// the pubsub fan-out goroutines that deliver packets to repeaters.
// All methods are internal to the hub package — external callers use
// Hub.Start(), Hub.Stop(), and Hub.ReloadRepeater().
type subscriptionManager struct {
	// subscriptions stores per-repeater maps of talkgroup/repeater → cancel func
	subscriptions *xsync.Map[uint, *xsync.Map[uint, *context.CancelFunc]]
	hub           *Hub
	mu            sync.Mutex
}

func newSubscriptionManager(h *Hub) *subscriptionManager {
	return &subscriptionManager{
		subscriptions: xsync.NewMap[uint, *xsync.Map[uint, *context.CancelFunc]](),
		hub:           h,
	}
}

// activateRepeater sets up all pubsub subscriptions for a repeater
// (private calls, static TGs, and dynamic TGs). Safe to call multiple times.
func (h *Hub) activateRepeater(ctx context.Context, repeaterID uint) {
	h.subscriptionMgr.mu.Lock()
	defer h.subscriptionMgr.mu.Unlock()
	h.activateRepeaterLocked(ctx, repeaterID)
}

// activateRepeaterLocked is the inner implementation of activateRepeater.
// Caller MUST hold h.subscriptionMgr.mu.
func (h *Hub) activateRepeaterLocked(ctx context.Context, repeaterID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.ActivateRepeater")
	defer span.End()

	_, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		h.subscriptionMgr.subscriptions.Store(repeaterID, xsync.NewMap[uint, *context.CancelFunc]())
	}

	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		slog.Error("Failed to load radio subscriptions", "repeaterID", repeaterID)
		return
	}

	p, err := models.FindRepeaterByID(h.db, repeaterID)
	if err != nil {
		slog.Error("Failed to find repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	// Subscribe to private calls for this repeater
	_, ok = radioSubs.Load(repeaterID)
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		radioSubs.Store(repeaterID, &cancel)
		//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
		go h.subscriptionMgr.subscribeRepeater(newCtx, repeaterID)
	}

	// Subscribe to each static talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(tg.ID, &cancel)
			//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
			go h.subscriptionMgr.subscribeTG(newCtx, repeaterID, tg.ID)
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(tg.ID, &cancel)
			//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
			go h.subscriptionMgr.subscribeTG(newCtx, repeaterID, tg.ID)
		}
	}

	// Subscribe to dynamic talkgroups
	if p.TS1DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS1DynamicTalkgroupID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(*p.TS1DynamicTalkgroupID, &cancel)
			//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
			go h.subscriptionMgr.subscribeTG(newCtx, repeaterID, *p.TS1DynamicTalkgroupID)
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS2DynamicTalkgroupID)
		if !ok {
			newCtx, cancel := context.WithCancel(context.Background())
			radioSubs.Store(*p.TS2DynamicTalkgroupID, &cancel)
			//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
			go h.subscriptionMgr.subscribeTG(newCtx, repeaterID, *p.TS2DynamicTalkgroupID)
		}
	}
}

// deactivateRepeater force-cancels all subscriptions for a specific repeater.
// Unlike cancelSubscription (which checks whether a TG is still needed),
// this unconditionally tears down every goroutine for the repeater.
func (h *Hub) deactivateRepeater(_ context.Context, repeaterID uint) {
	h.subscriptionMgr.mu.Lock()
	defer h.subscriptionMgr.mu.Unlock()
	h.deactivateRepeaterLocked(repeaterID)
}

// deactivateRepeaterLocked is the inner implementation of deactivateRepeater.
// Caller MUST hold h.subscriptionMgr.mu.
func (h *Hub) deactivateRepeaterLocked(repeaterID uint) {
	slog.Debug("Deactivating repeater subscriptions", "repeaterID", repeaterID)
	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	// Force-cancel every subscription goroutine (TG + repeater-direct).
	radioSubs.Range(func(key uint, cancelPtr *context.CancelFunc) bool {
		if cancelPtr != nil {
			(*cancelPtr)()
		}
		radioSubs.Delete(key)
		return true
	})
	h.subscriptionMgr.subscriptions.Delete(repeaterID)
}

// StopAllRepeaters cancels all subscriptions for all repeaters (used at shutdown).
func (h *Hub) stopAllRepeaters(ctx context.Context) {
	slog.Debug("Cancelling all subscriptions")
	h.subscriptionMgr.subscriptions.Range(func(radioID uint, _ *xsync.Map[uint, *context.CancelFunc]) bool {
		h.deactivateRepeater(ctx, radioID)
		return true
	})
}

// subscribeTalkgroup subscribes a repeater to a specific talkgroup.
func (h *Hub) subscribeTalkgroup(ctx context.Context, repeaterID uint, talkgroupID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.SubscribeTalkgroup")
	defer span.End()
	h.subscriptionMgr.mu.Lock()
	defer h.subscriptionMgr.mu.Unlock()

	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	_, ok = radioSubs.Load(talkgroupID)
	if !ok {
		newCtx, cancel := context.WithCancel(context.Background())
		radioSubs.Store(talkgroupID, &cancel)
		//nolint:contextcheck // subscription goroutine outlives the caller; must not inherit caller's context
		go h.subscriptionMgr.subscribeTG(newCtx, repeaterID, talkgroupID)
	}
}

// unsubscribeTalkgroup cancels a specific talkgroup subscription for a repeater.
func (h *Hub) unsubscribeTalkgroup(repeaterID uint, talkgroupID uint, slot dmrconst.Timeslot) {
	h.subscriptionMgr.cancelSubscription(repeaterID, talkgroupID, slot)
}

// ListenForWebsocket relays call events to a WebSocket client for a specific user.
func (h *Hub) ListenForWebsocket(ctx context.Context, userID uint) {
	slog.Debug("Listening for websocket", "userID", userID)
	subscription := h.pubsub.Subscribe("calls")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			slog.Debug("Websocket context done", "userID", userID)
			return
		case msg := <-pubsubChannel:
			if msg == nil {
				continue
			}
			slog.Info("Received call message for websocket", "userID", userID, "message", string(msg))
			var call models.Call
			err := json.Unmarshal(msg, &call)
			if err != nil {
				slog.Error("Error unmarshalling call", "error", err)
				continue
			}

			userExists, err := models.UserIDExists(h.db, userID)
			if err != nil {
				slog.Error("Error checking if user exists", "userID", userID, "error", err)
				continue
			}

			if !userExists {
				slog.Error("User does not exist", "userID", userID)
				continue
			}

			user, err := models.FindUserByID(h.db, userID)
			if err != nil {
				slog.Error("Error finding user", "userID", userID, "error", err)
				continue
			}

			for _, p := range user.Repeaters {
				want, _ := p.WantRXCall(call)
				if want || call.User.ID == userID || call.DestinationID == p.OwnerID {
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
					callJSON, err := json.Marshal(jsonCall)
					if err != nil {
						slog.Error("Error marshalling call JSON", "error", err)
						break
					}
					if err := h.pubsub.Publish(fmt.Sprintf("calls:%d", userID), callJSON); err != nil {
						slog.Error("Error publishing call", "userID", userID, "error", err)
						continue
					}
					break
				}
			}
		}
	}
}

// cancelSubscription cancels a single talkgroup subscription, checking that no other
// slot or static link still needs it before actually cancelling.
func (m *subscriptionManager) cancelSubscription(repeaterID uint, talkgroupID uint, slot dmrconst.Timeslot) {
	radioSubscriptions, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		slog.Error("Failed to load radio subscriptions for repeater", "repeaterID", repeaterID)
		return
	}

	p, err := models.FindRepeaterByID(m.hub.db, repeaterID)
	if err != nil {
		slog.Error("Failed to find repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	// Check the other slot
	dynamicSlot := p.TS2DynamicTalkgroupID
	if slot == dmrconst.TimeslotTwo {
		dynamicSlot = p.TS1DynamicTalkgroupID
	}

	// If the other slot is linked to this talkgroup, don't cancel the subscription
	if dynamicSlot != nil && *dynamicSlot == talkgroupID {
		slog.Debug("Not cancelling subscription because the other slot is linked to this talkgroup",
			"repeaterID", p.ID, "talkgroupID", talkgroupID, "slot", slot)
		return
	}

	for _, tg := range p.TS1StaticTalkgroups {
		if tg.ID == talkgroupID {
			return
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		if tg.ID == talkgroupID {
			return
		}
	}
	cancelPtr, ok := radioSubscriptions.LoadAndDelete(talkgroupID)
	if !ok {
		return
	}
	cancel := *cancelPtr
	cancel()
}

// subscribeRepeater listens for private calls destined to a specific repeater
// and delivers them to the appropriate server via deliverToServer.
func (m *subscriptionManager) subscribeRepeater(ctx context.Context, repeaterID uint) {
	slog.Debug("Listening for calls on repeater", "repeaterID", repeaterID)
	subscription := m.hub.pubsub.Subscribe(fmt.Sprintf("hub:packets:repeater:%d", repeaterID))
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for {
		select {
		case <-ctx.Done():
			slog.Debug("Context canceled, stopping subscription to hub:packets:repeater", "repeaterID", repeaterID)
			radioSubs, ok := m.subscriptions.Load(repeaterID)
			if ok {
				radioSubs.Delete(repeaterID)
			}
			return
		case msg := <-pubsubChannel:
			if msg == nil {
				return
			}

			rawPacket := models.RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg(msg)
			if err != nil {
				slog.Error("Failed to unmarshal raw packet", "error", err)
				continue
			}
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				slog.Error("Failed to unpack packet")
				continue
			}
			packet.Repeater = repeaterID

			p, err := models.FindRepeaterByID(m.hub.db, repeaterID)
			if err != nil {
				slog.Error("Failed to find repeater for delivery", "repeaterID", repeaterID, "error", err)
				continue
			}
			m.hub.deliverToServer(p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
		}
	}
}

// subscribeTG listens for group calls on a talkgroup and delivers matching
// packets to the repeater via the appropriate server.
func (m *subscriptionManager) subscribeTG(ctx context.Context, repeaterID uint, tg uint) {
	if tg == 0 {
		return
	}
	slog.Debug("Listening for calls on talkgroup", "repeaterID", repeaterID, "talkgroupID", tg)
	subscription := m.hub.pubsub.Subscribe(fmt.Sprintf("hub:packets:talkgroup:%d", tg))
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()
	pubsubChannel := subscription.Channel()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Context canceled, stopping subscription to hub:packets:talkgroup", "repeaterID", repeaterID, "talkgroupID", tg)
			radioSubs, ok := m.subscriptions.Load(repeaterID)
			if ok {
				radioSubs.Delete(tg)
			}
			return
		case msg := <-pubsubChannel:
			if msg == nil {
				return
			}

			rawPacket := models.RawDMRPacket{}
			_, err := rawPacket.UnmarshalMsg(msg)
			if err != nil {
				slog.Error("Failed to unmarshal raw packet", "error", err)
				continue
			}
			packet, ok := models.UnpackPacket(rawPacket.Data)
			if !ok {
				slog.Error("Failed to unpack packet")
				continue
			}

			p, err := models.FindRepeaterByID(m.hub.db, repeaterID)
			if err != nil {
				slog.Error("Failed to find repeater", "repeaterID", repeaterID, "error", err)
				continue
			}

			if packet.Repeater == repeaterID {
				// Simplex repeaters echo their own packets back on the opposite timeslot
				if !p.SimplexRepeater {
					continue
				}
				packet.Repeater = p.ID
				packet.Slot = !packet.Slot

				m.hub.deliverToServer(p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
				continue
			}

			want, slot := p.WantRX(packet)
			if want {
				packet.Repeater = p.ID
				packet.Slot = slot

				m.hub.deliverToServer(p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
			} else {
				err = subscription.Close()
				if err != nil {
					slog.Error("Error closing pubsub connection", "error", err)
				}
				return
			}
		}
	}
}

// subscribeBroadcast listens on the hub:packets:broadcast topic and delivers
// packets to the named server, skipping packets that originated from it.
func (m *subscriptionManager) subscribeBroadcast(serverName string) {
	slog.Debug("Subscribing to broadcast for server", "server", serverName)
	subscription := m.hub.pubsub.Subscribe("hub:packets:broadcast")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing broadcast pubsub connection", "error", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for msg := range pubsubChannel {
		if len(msg) < 1 {
			continue
		}
		nameLen := int(msg[0])
		if len(msg) < 1+nameLen {
			continue
		}
		sourceName := string(msg[1 : 1+nameLen])
		packetData := msg[1+nameLen:]

		// Skip echo
		if sourceName == serverName {
			continue
		}

		packet, ok := models.UnpackPacket(packetData)
		if !ok {
			slog.Error("Failed to unpack broadcast packet")
			continue
		}

		m.hub.deliverToServer(serverName, RoutedPacket{RepeaterID: 0, Packet: packet})
	}
}

// subscribePeers listens on the hub:packets:peers topic and delivers
// packets to the named peer server.
func (m *subscriptionManager) subscribePeers(serverName string) {
	slog.Debug("Subscribing to peers for server", "server", serverName)
	subscription := m.hub.pubsub.Subscribe("hub:packets:peers")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing peers pubsub connection", "error", err)
		}
	}()
	pubsubChannel := subscription.Channel()
	for msg := range pubsubChannel {
		rawPacket := models.RawDMRPacket{}
		_, err := rawPacket.UnmarshalMsg(msg)
		if err != nil {
			slog.Error("Failed to unmarshal peer packet", "error", err)
			continue
		}
		packet, ok := models.UnpackPacket(rawPacket.Data)
		if !ok {
			slog.Error("Failed to unpack peer packet")
			continue
		}

		// The packet.Repeater field holds the peer ID
		m.hub.deliverToServer(serverName, RoutedPacket{RepeaterID: packet.Repeater, Packet: packet})
	}
}
