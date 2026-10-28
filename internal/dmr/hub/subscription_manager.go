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
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

// subscriptionManager tracks repeater-to-talkgroup subscriptions and manages
// the pubsub fan-out goroutines that deliver packets to repeaters.
// All methods are internal to the hub package — external callers use
// Hub.Start(), Hub.Stop(), and Hub.ReloadRepeater().
type subscriptionManager struct {
	// subscriptions stores per-repeater maps of talkgroup/repeater → subscription
	subscriptions *xsync.Map[uint, *xsync.Map[uint, *subscription]]
	feeds         *xsync.Map[uint, *repeaterFeed]
	hub           *Hub
	mu            sync.Mutex
}

func newSubscriptionManager(h *Hub) *subscriptionManager {
	return &subscriptionManager{
		subscriptions: xsync.NewMap[uint, *xsync.Map[uint, *subscription]](),
		feeds:         xsync.NewMap[uint, *repeaterFeed](),
		hub:           h,
	}
}

// repeaterFeed is the one feed that brings a repeater its private calls
// and the calls of its talkgroups. One goroutine reads it, so the
// repeater gets the bursts of all its topics in the order they were
// published, and the calls of a source reach it in the order they came.
// Each repeater has its own, so a slow repeater holds up no other.
type repeaterFeed struct {
	feed pubsub.Feed
	mu   sync.Mutex
	subs map[string]*subscription
}

func (f *repeaterFeed) add(sub *subscription) {
	f.mu.Lock()
	f.subs[sub.topic] = sub
	f.mu.Unlock()
	f.feed.Add(sub.topic)
}

func (f *repeaterFeed) remove(sub *subscription) {
	f.mu.Lock()
	if f.subs[sub.topic] != sub {
		f.mu.Unlock()
		return
	}
	delete(f.subs, sub.topic)
	f.mu.Unlock()
	f.feed.Remove(sub.topic)
}

func (f *repeaterFeed) of(topic string) *subscription {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.subs[topic]
}

// subscription is one topic of the feed of a repeater: its private calls,
// or a talkgroup. Once stop returns, the feed delivers nothing more of it,
// even a packet it was already handling.
type subscription struct {
	done    <-chan struct{}
	cancel  context.CancelFunc
	feed    *repeaterFeed
	topic   string
	tg      uint
	flood   bool
	mu      sync.RWMutex
	stopped bool
}

// subscribe adds the topic of the private calls of repeaterID to the feed,
// and returns its subscription.
func (f *repeaterFeed) subscribe(repeaterID uint) *subscription {
	sub := f.newSubscription(repeaterTopic(repeaterID), 0)
	f.add(sub)
	return sub
}

// subscribeFlood adds the topic of the private traffic that floods every
// repeater to the feed, and returns its subscription.
func (f *repeaterFeed) subscribeFlood() *subscription {
	sub := f.newSubscription(privateFloodTopic, 0)
	sub.flood = true
	f.add(sub)
	return sub
}

// subscribeTG adds the topic of the talkgroup tg to the feed, and returns
// its subscription. Talkgroup 0 gets one that delivers nothing.
func (f *repeaterFeed) subscribeTG(tg uint) *subscription {
	if tg == 0 {
		return f.newSubscription("", 0)
	}
	sub := f.newSubscription(talkgroupTopic(tg), tg)
	f.add(sub)
	return sub
}

func (f *repeaterFeed) newSubscription(topic string, tg uint) *subscription {
	ctx, cancel := context.WithCancel(context.Background())
	return &subscription{done: ctx.Done(), cancel: cancel, feed: f, tg: tg, topic: topic}
}

// stop cancels the subscription, removes its topic from the feed and
// waits for a delivery in progress, which the cancel unblocks.
func (s *subscription) stop() {
	s.cancel()
	s.mu.Lock()
	s.stopped = true
	s.mu.Unlock()
	s.feed.remove(s)
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

const (
	privateFloodTopic = "hub:packets:private-flood"
	floodSubKey       = uint(1<<32 - 1)
)

func repeaterTarget(repeaterID uint) string {
	return "repeater:" + strconv.FormatUint(uint64(repeaterID), 10)
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
		go h.rulesOf(repeaterID)
	}

	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		slog.Error("Failed to load radio subscriptions", "repeaterID", repeaterID)
		return
	}
	feed := h.subscriptionMgr.feedOf(repeaterID)

	p, err := models.FindRepeaterByID(h.db, repeaterID)
	if err != nil {
		slog.Error("Failed to find repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	// Subscribe to private calls for this repeater
	_, ok = radioSubs.Load(repeaterID)
	if !ok {
		radioSubs.Store(repeaterID, feed.subscribe(repeaterID))
	}

	if _, ok = radioSubs.Load(floodSubKey); !ok {
		radioSubs.Store(floodSubKey, feed.subscribeFlood())
	}

	// Subscribe to each static talkgroup
	for _, tg := range p.TS1StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			radioSubs.Store(tg.ID, feed.subscribeTG(tg.ID))
		}
	}
	for _, tg := range p.TS2StaticTalkgroups {
		_, ok := radioSubs.Load(tg.ID)
		if !ok {
			radioSubs.Store(tg.ID, feed.subscribeTG(tg.ID))
		}
	}

	// Subscribe to dynamic talkgroups
	if p.TS1DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS1DynamicTalkgroupID)
		if !ok {
			radioSubs.Store(*p.TS1DynamicTalkgroupID, feed.subscribeTG(*p.TS1DynamicTalkgroupID))
		}
	}
	if p.TS2DynamicTalkgroupID != nil {
		_, ok := radioSubs.Load(*p.TS2DynamicTalkgroupID)
		if !ok {
			radioSubs.Store(*p.TS2DynamicTalkgroupID, feed.subscribeTG(*p.TS2DynamicTalkgroupID))
		}
	}
}

// resubscribeLocked makes the subscriptions of the active repeater match
// the database. It keeps the feed of the repeater and the subscriptions
// that the repeater still wants, so a burst published while it runs is
// delivered once. A repeater that is no longer in the database keeps no
// subscription; when the database fails, the subscriptions stay as they
// were. Caller MUST hold h.subscriptionMgr.mu.
func (h *Hub) resubscribeLocked(ctx context.Context, repeaterID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.ResubscribeRepeater")
	defer span.End()

	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	want := map[uint]bool{}
	p, err := models.FindRepeaterByID(h.db, repeaterID)
	switch {
	case errors.Is(err, gorm.ErrRecordNotFound):
	case err != nil:
		slog.Error("Failed to find repeater to reload, keeping its subscriptions", "repeaterID", repeaterID, "error", err)
		return
	default:
		want = wantedSubscriptions(&p)
	}

	var stale []uint
	radioSubs.Range(func(key uint, _ *subscription) bool {
		if !want[key] {
			stale = append(stale, key)
		}
		return true
	})
	for _, key := range stale {
		if sub, ok := radioSubs.LoadAndDelete(key); ok {
			sub.stop()
		}
	}
	if len(want) == 0 {
		return
	}
	feed := h.subscriptionMgr.feedOf(repeaterID)
	for key := range want {
		if _, ok := radioSubs.Load(key); ok {
			continue
		}
		switch key {
		case repeaterID:
			radioSubs.Store(key, feed.subscribe(repeaterID))
		case floodSubKey:
			radioSubs.Store(key, feed.subscribeFlood())
		default:
			radioSubs.Store(key, feed.subscribeTG(key))
		}
	}
}

// wantedSubscriptions returns the keys of the subscriptions that the
// repeater p wants: its private calls, the private flood, and its static
// and dynamic talkgroups.
func wantedSubscriptions(p *models.Repeater) map[uint]bool {
	want := map[uint]bool{p.ID: true, floodSubKey: true}
	for _, tg := range p.TS1StaticTalkgroups {
		want[tg.ID] = true
	}
	for _, tg := range p.TS2StaticTalkgroups {
		want[tg.ID] = true
	}
	if p.TS1DynamicTalkgroupID != nil {
		want[*p.TS1DynamicTalkgroupID] = true
	}
	if p.TS2DynamicTalkgroupID != nil {
		want[*p.TS2DynamicTalkgroupID] = true
	}
	return want
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
	if feed, ok := h.subscriptionMgr.feeds.LoadAndDelete(repeaterID); ok {
		if err := feed.feed.Close(); err != nil {
			slog.Error("Error closing the feed of a repeater", "repeaterID", repeaterID, "error", err)
		}
	}
	select {
	case h.left <- struct{}{}:
	default:
	}
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
	feed, ok := h.subscriptionMgr.feeds.Load(repeaterID)
	if !ok {
		return
	}
	_, ok = radioSubs.Load(talkgroupID)
	if !ok {
		radioSubs.Store(talkgroupID, feed.subscribeTG(talkgroupID))
	}
}

// unsubscribeTalkgroup cancels a specific talkgroup subscription for a repeater.
func (h *Hub) unsubscribeTalkgroup(repeaterID uint, talkgroupID uint, slot dmrconst.Timeslot) {
	h.subscriptionMgr.cancelSubscription(repeaterID, talkgroupID, slot)
}

// ListenForWebsocket relays call events to a WebSocket client for a specific user.
// It caches the user and its repeaters, and reloads them when one of those
// repeaters changes. It reads the SMS Storage setting again for each data
// call. A change of the user or of the repeaters it owns closes the
// websocket instead.
func (h *Hub) ListenForWebsocket(ctx context.Context, userID uint) {
	slog.Debug("Listening for websocket", "userID", userID)

	changes := h.pubsub.Subscribe(repeaterChangedTopic)
	defer func() {
		if err := changes.Close(); err != nil {
			slog.Error("Error closing repeater change subscription", "error", err)
		}
	}()

	user, err := models.FindUserWithRepeaterTalkgroups(h.db, userID)
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
	changesChannel := changes.Channel()

	for {
		select {
		case <-ctx.Done():
			slog.Debug("Websocket context done", "userID", userID)
			return
		case msg, ok := <-changesChannel:
			if !ok {
				return
			}
			_, id, ok := parseRepeaterChange(msg)
			if !ok || !ownsRepeater(&user, id) {
				continue
			}
			refreshed, err := models.FindUserWithRepeaterTalkgroups(h.db, userID)
			if err != nil {
				slog.Error("Error reloading user for websocket, keeping stale cache", "userID", userID, "error", err)
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
			if call.Kind == models.CallKindData || call.Kind == models.CallKindUDT {
				if err := models.ReloadSMSStorage(h.db, &user); err != nil {
					slog.Error("Error reading SMS Storage for websocket", "userID", userID, "error", err)
					continue
				}
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

func ownsRepeater(user *models.User, repeaterID uint) bool {
	for i := range user.Repeaters {
		if user.Repeaters[i].ID == repeaterID {
			return true
		}
	}
	return false
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
	packet.TrackID = rawPacket.TrackID
	packet.Codec, packet.Copy, packet.HasCopy = rawPacket.Codec, rawPacket.Copy, rawPacket.HasCopy
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

// forget removes the entry of a repeater's subscription under key, unless
// the entry already belongs to a newer subscription.
func (m *subscriptionManager) forget(repeaterID uint, key uint, sub *subscription) {
	radioSubs, ok := m.subscriptions.Load(repeaterID)
	if !ok {
		return
	}
	radioSubs.Compute(key, func(old *subscription, loaded bool) (*subscription, xsync.ComputeOp) {
		if loaded && old == sub {
			return nil, xsync.DeleteOp
		}
		return old, xsync.CancelOp
	})
}

// feedOf returns the feed of a repeater, and starts it when it has none.
// The caller holds m.mu.
func (m *subscriptionManager) feedOf(repeaterID uint) *repeaterFeed {
	if f, ok := m.feeds.Load(repeaterID); ok {
		return f
	}
	f := &repeaterFeed{feed: m.hub.pubsub.Feed(), subs: map[string]*subscription{}}
	m.feeds.Store(repeaterID, f)
	go m.readFeed(f, repeaterID)
	return f
}

// readFeed delivers the messages of the feed of a repeater until it
// closes.
func (m *subscriptionManager) readFeed(f *repeaterFeed, repeaterID uint) {
	slog.Debug("Listening for calls on repeater", "repeaterID", repeaterID)
	for msg := range f.feed.Channel() {
		sub := f.of(msg.Topic)
		if sub == nil {
			continue
		}
		packet, ok := unmarshalRawPacket(msg.Payload)
		if !ok {
			continue
		}
		if sub.flood && packet.Repeater == repeaterID {
			continue
		}
		if sub.tg == 0 {
			m.deliverPrivate(sub, packet, repeaterID)
		} else {
			m.deliverGroup(sub, packet, repeaterID)
		}
	}
}

// deliverPrivate delivers a private call to the repeater.
func (m *subscriptionManager) deliverPrivate(sub *subscription, packet models.Packet, repeaterID uint) {
	packet.Repeater = repeaterID
	p, err := m.hub.routingRepeater(repeaterID)
	if err != nil {
		slog.Error("Failed to find repeater for delivery", "repeaterID", repeaterID, "error", err)
		return
	}
	if m.hub.refusal(repeaterID, packet.Slot, packet, true) != "" {
		return
	}
	if !wantsVersion(&packet, p.SlotCodec(packet.Slot)) {
		return
	}
	sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
	m.hub.noteCopy(&packet, repeaterTarget(repeaterID))
}

// deliverGroup delivers a group call of the talkgroup of sub to the
// repeater, and ends sub when the repeater no longer listens to it.
func (m *subscriptionManager) deliverGroup(sub *subscription, packet models.Packet, repeaterID uint) {
	p, err := m.hub.routingRepeater(repeaterID)
	if err != nil {
		slog.Error("Failed to find repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	if packet.Repeater == repeaterID {
		// Simplex repeaters echo their own packets back on the opposite timeslot
		if !p.SimplexRepeater {
			return
		}
		packet.Repeater = p.ID
		packet.Slot = !packet.Slot
		if !wantsVersion(&packet, p.SlotCodec(packet.Slot)) {
			return
		}
		sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
		m.hub.noteCopy(&packet, repeaterTarget(repeaterID))
		return
	}

	want, slot := p.WantRX(packet)
	if !want {
		m.forget(repeaterID, sub.tg, sub)
		sub.stop()
		return
	}
	if m.hub.refusal(repeaterID, slot, packet, true) != "" {
		return
	}
	packet.Repeater = p.ID
	packet.Slot = slot
	if !wantsVersion(&packet, p.SlotCodec(slot)) {
		return
	}
	sub.deliver(m.hub, p.Type, RoutedPacket{RepeaterID: repeaterID, Packet: packet})
	m.hub.noteCopy(&packet, repeaterTarget(repeaterID))
}
