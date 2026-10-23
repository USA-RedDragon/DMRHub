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
	"strconv"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
)

// userCacheRefreshInterval controls how often ListenForWebsocket refreshes
// the cached user and repeater list from the database.
const userCacheRefreshInterval = 30 * time.Second

// subscriptionManager tracks repeater-to-talkgroup subscriptions and manages
// the pubsub fan-out goroutines that deliver packets to repeaters.
// All methods are internal to the hub package — external callers use
// Hub.Start(), Hub.Stop(), and Hub.ReloadRepeater().
type subscriptionManager struct {
	// subscriptions stores per-repeater maps of talkgroup/repeater → subscription
	subscriptions *xsync.Map[uint, *xsync.Map[uint, *subscription]]
	hub           *Hub
	mu            sync.Mutex
}

func newSubscriptionManager(h *Hub) *subscriptionManager {
	return &subscriptionManager{
		subscriptions: xsync.NewMap[uint, *xsync.Map[uint, *subscription]](),
		hub:           h,
	}
}

// subscription is one fan-out goroutine of a repeater. Once stop returns,
// the goroutine delivers nothing more, even a packet it was already handling.
type subscription struct {
	done    <-chan struct{}
	cancel  context.CancelFunc
	mu      sync.RWMutex
	stopped bool
}

func newSubscription() *subscription {
	ctx, cancel := context.WithCancel(context.Background())
	return &subscription{done: ctx.Done(), cancel: cancel}
}

// stop cancels the subscription and waits for a delivery in progress, which
// the cancel unblocks.
func (s *subscription) stop() {
	s.cancel()
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
}

// deliver hands rp to the server unless the subscription is stopped.
func (s *subscription) deliver(h *Hub, serverName string, rp RoutedPacket) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.stopped {
		return
	}
	h.deliverToServerUnless(s.done, serverName, rp)
}

func repeaterTopic(repeaterID uint) string {
	return repeaterTopicPrefix + strconv.FormatUint(uint64(repeaterID), 10)
}

func talkgroupTopic(tg uint) string {
	return "hub:packets:talkgroup:" + strconv.FormatUint(uint64(tg), 10)
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
		h.subscriptionMgr.subscriptions.Store(repeaterID, xsync.NewMap[uint, *subscription]())
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
		sub := newSubscription()
		radioSubs.Store(repeaterID, sub)
		go h.subscriptionMgr.subscribeRepeater(sub, h.pubsub.Subscribe(repeaterTopic(repeaterID)), repeaterID)
	}

	// Subscribe to each static talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			sub := newSubscription()
			radioSubs.Store(tg.ID, sub)
			go h.subscriptionMgr.subscribeTG(sub, h.pubsub.Subscribe(talkgroupTopic(tg.ID)), repeaterID, tg.ID)
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			sub := newSubscription()
			radioSubs.Store(tg.ID, sub)
			go h.subscriptionMgr.subscribeTG(sub, h.pubsub.Subscribe(talkgroupTopic(tg.ID)), repeaterID, tg.ID)
		}
	}

	// Subscribe to dynamic talkgroups
	if p.TS1DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS1DynamicTalkgroupID)
		if !ok {
			sub := newSubscription()
			radioSubs.Store(*p.TS1DynamicTalkgroupID, sub)
			go h.subscriptionMgr.subscribeTG(sub, h.pubsub.Subscribe(talkgroupTopic(*p.TS1DynamicTalkgroupID)), repeaterID, *p.TS1DynamicTalkgroupID)
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS2DynamicTalkgroupID)
		if !ok {
			sub := newSubscription()
			radioSubs.Store(*p.TS2DynamicTalkgroupID, sub)
			go h.subscriptionMgr.subscribeTG(sub, h.pubsub.Subscribe(talkgroupTopic(*p.TS2DynamicTalkgroupID)), repeaterID, *p.TS2DynamicTalkgroupID)
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
	// Collect all keys first to avoid modifying the map during Range,
	// which can cause entries to be skipped.
	var keys []uint
	radioSubs.Range(func(key uint, _ *subscription) bool {
		keys = append(keys, key)
		return true
	})
	for _, key := range keys {
		if sub, ok := radioSubs.LoadAndDelete(key); ok {
			sub.stop()
		}
	}
	h.subscriptionMgr.subscriptions.Delete(repeaterID)
}

// stopAllRepeaters cancels all subscriptions for all repeaters (used at shutdown).
func (h *Hub) stopAllRepeaters(ctx context.Context) {
	slog.Debug("Cancelling all subscriptions")
	// Collect all repeater IDs first to avoid modifying the map during
	// Range iteration, which can cause entries to be skipped.
	var ids []uint
	h.subscriptionMgr.subscriptions.Range(func(radioID uint, _ *xsync.Map[uint, *subscription]) bool {
		ids = append(ids, radioID)
		return true
	})
	for _, radioID := range ids {
		h.deactivateRepeater(ctx, radioID)
	}
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
		sub := newSubscription()
		radioSubs.Store(talkgroupID, sub)
		go h.subscriptionMgr.subscribeTG(sub, h.pubsub.Subscribe(talkgroupTopic(talkgroupID)), repeaterID, talkgroupID)
	}
}

// unsubscribeTalkgroup cancels a specific talkgroup subscription for a repeater.
func (h *Hub) unsubscribeTalkgroup(repeaterID uint, talkgroupID uint, slot dmrconst.Timeslot) {
	h.subscriptionMgr.cancelSubscription(repeaterID, talkgroupID, slot)
}

// ListenForWebsocket relays call events to a WebSocket client for a specific user.
// It caches the user's repeater list and refreshes it periodically to avoid
// hitting the database on every pubsub message.
func (h *Hub) ListenForWebsocket(ctx context.Context, userID uint) {
	slog.Debug("Listening for websocket", "userID", userID)

	// Load the user and repeaters once up front.
	user, err := models.FindUserByID(h.db, userID)
	if err != nil {
		slog.Error("User not found for websocket, aborting", "userID", userID, "error", err)
		return
	}

	subscription := h.pubsub.Subscribe("calls")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()

	pubsubChannel := subscription.Channel()
	refreshTicker := time.NewTicker(userCacheRefreshInterval)
	defer refreshTicker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Websocket context done", "userID", userID)
			return
		case <-refreshTicker.C:
			// Periodically refresh the cached user and repeaters.
			refreshed, err := models.FindUserByID(h.db, userID)
			if err != nil {
				slog.Error("Error refreshing user for websocket, keeping stale cache", "userID", userID, "error", err)
				continue
			}
			user = refreshed
		case msg := <-pubsubChannel:
			if msg == nil {
				continue
			}
			slog.Debug("Received call message for websocket", "userID", userID)
			var call models.Call
			err := json.Unmarshal(msg, &call)
			if err != nil {
				slog.Error("Error unmarshalling call", "error", err)
				continue
			}

			if !wantsCall(&user, &call) {
				continue
			}
			rows, err := apimodels.NewWSCallResponsesFor(h.db, &user, models.CurrentSMSWindow(), []models.Call{call})
			if err != nil || len(rows) != 1 {
				slog.Error("Error showing call", "userID", userID, "error", err)
				continue
			}
			callJSON, err := json.Marshal(rows[0])
			if err != nil {
				slog.Error("Error marshalling call JSON", "error", err)
				continue
			}
			if err := h.pubsub.Publish(fmt.Sprintf("calls:%d", userID), callJSON); err != nil {
				slog.Error("Error publishing call", "userID", userID, "error", err)
			}
		}
	}
}

func wantsCall(user *models.User, call *models.Call) bool {
	if call.SourceID == user.ID || call.User.ID == user.ID {
		return true
	}
	if !call.GroupCall {
		if call.DestinationID == user.ID {
			return true
		}
		for i := range user.Repeaters {
			if call.DestinationID == user.Repeaters[i].ID {
				return true
			}
		}
		return false
	}
	for i := range user.Repeaters {
		if want, _ := user.Repeaters[i].WantRXCall(*call); want {
			return true
		}
	}
	return false
}

// unmarshalRawPacket decodes a pubsub message containing a serialized
// RawDMRPacket into a models.Packet. Returns the packet and true on success.
func unmarshalRawPacket(msg []byte) (models.Packet, bool) {
	var rawPacket models.RawDMRPacket
	_, err := rawPacket.UnmarshalMsg(msg)
	if err != nil {
		slog.Error("Failed to unmarshal raw packet", "error", err)
		return models.Packet{}, false
	}
	packet, ok := models.UnpackPacket(rawPacket.Data)
	if !ok {
		slog.Error("Failed to unpack packet")
		return models.Packet{}, false
	}
	return packet, true
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
	sub, ok := radioSubscriptions.LoadAndDelete(talkgroupID)
	if !ok {
		return
	}
	sub.stop()
}

// subscribeRepeater listens for private calls destined to a specific repeater
// and delivers them to the appropriate server via deliverToServer.
func (m *subscriptionManager) subscribeRepeater(sub *subscription, feed pubsub.Subscription, repeaterID uint) {
	slog.Debug("Listening for calls on repeater", "repeaterID", repeaterID)
	defer func() {
		err := feed.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()
	pubsubChannel := feed.Channel()
	for {
		select {
		case <-sub.done:
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

			packet, ok := unmarshalRawPacket(msg)
			if !ok {
				continue
			}
			packet.Repeater = repeaterID

			p, err := models.FindRepeaterByID(m.hub.db, repeaterID)
			if err != nil {
				slog.Error("Failed to find repeater for delivery", "repeaterID", repeaterID, "error", err)
				continue
			}
			sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
		}
	}
}

// subscribeTG listens for group calls on a talkgroup and delivers matching
// packets to the repeater via the appropriate server.
func (m *subscriptionManager) subscribeTG(sub *subscription, feed pubsub.Subscription, repeaterID uint, tg uint) {
	defer func() {
		err := feed.Close()
		if err != nil {
			slog.Error("Error closing pubsub connection", "error", err)
		}
	}()
	if tg == 0 {
		return
	}
	slog.Debug("Listening for calls on talkgroup", "repeaterID", repeaterID, "talkgroupID", tg)
	pubsubChannel := feed.Channel()

	for {
		select {
		case <-sub.done:
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

			packet, ok := unmarshalRawPacket(msg)
			if !ok {
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

				sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
				continue
			}

			want, slot := p.WantRX(packet)
			if want {
				packet.Repeater = p.ID
				packet.Slot = slot

				sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
			} else {
				// Clean up the subscription map entry and cancel the context
				// to avoid leaking resources.
				radioSubs, ok := m.subscriptions.Load(repeaterID)
				if ok {
					if loaded, ok := radioSubs.LoadAndDelete(tg); ok {
						loaded.stop()
					}
				}
				err = feed.Close()
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
