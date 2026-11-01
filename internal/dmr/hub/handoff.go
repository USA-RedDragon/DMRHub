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
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// StopFor stops the subscriptions of the repeater id on this hub, which
// another replica claimed, once the feed of the repeater here got every
// packet that was published before that replica subscribed, and returns
// once the server named server has taken every packet that the feed
// handed it. A server that hands the repeater over then knows each packet
// that it got for it.
func (h *Hub) StopFor(server string, id uint) {
	h.settle(id)
	h.deactivateRepeater(context.Background(), id)
	h.Barrier(server)
}

// settle returns once the feed of the repeater id has every message that
// was published before the call, or once the feed or the hub stops. It
// publishes a marker on the private topic of the repeater, which the feed
// takes, and waits for the feed to get it: the feed is one Redis
// connection, and Redis passes the messages of all its channels to one
// connection in the order that it ran their PUBLISH commands, so every
// message that Redis took before the marker comes before it. The
// replica that claimed the repeater subscribed before it claimed, and
// this replica publishes the marker after it got the claim, so the
// messages that only this replica gets come first. The in-memory pubsub
// hands a message to each feed when it is published, in the same order.
func (h *Hub) settle(id uint) {
	m := h.subscriptionMgr
	if h.pubsub == nil {
		return
	}
	if _, ok := m.feeds.Load(id); !ok {
		return
	}
	token := h.instanceID + ":" + strconv.FormatUint(m.settleSeq.Add(1), 10)
	mk := &settleMarker{repeaterID: id, came: make(chan struct{})}
	m.settles.Store(token, mk)
	defer m.settles.Delete(token)
	if _, ok := m.feeds.Load(id); !ok {
		return
	}
	if err := h.pubsub.Publish(repeaterTopic(id), []byte(settleMarkerPrefix+token)); err != nil {
		slog.Warn("failed to publish a settle marker", "repeaterID", id, "error", err)
		return
	}
	select {
	case <-mk.came:
	case <-h.done:
	}
}

// Barrier returns once the server named server has taken every packet that
// the hub handed it before the call, or once the server or the hub stops.
// A server that hands a repeater over stops the subscriptions of the
// repeater and then calls it, so that it knows every packet that it got for
// the repeater. It has no time bound: a server that a starved machine
// slows down still takes the packets, and the handover must hold them.
func (h *Hub) Barrier(server string) {
	h.barrierUnless(server, nil)
}

func (h *Hub) barrierUnless(server string, quit <-chan struct{}) bool {
	h.mu.RLock()
	entry, ok := h.servers[server]
	h.mu.RUnlock()
	if !ok {
		return true
	}
	done := make(chan struct{})
	go h.deliverToServerUnless(quit, server, RoutedPacket{Barrier: done})
	select {
	case <-done:
		return true
	case <-entry.gone:
		return true
	case <-quit:
		return false
	case <-h.done:
		return false
	}
}

func handoverTopic(server string, id uint) string {
	return "hub:handover:" + server + ":" + strconv.FormatUint(uint64(id), 10)
}

// HandOver sends report to the server named server of the replica that took
// over the repeater id from this one: what the server here knows that the
// server there needs to go on where this one stopped.
func (h *Hub) HandOver(server string, id uint, report []byte) {
	if h.pubsub == nil {
		return
	}
	if err := h.pubsub.Publish(handoverTopic(server, id), report); err != nil {
		slog.Warn("failed to publish a handover", "repeaterID", id, "error", err)
	}
}

func earlyHandoverTopic(server string, id uint) string {
	return "hub:handover-early:" + server + ":" + strconv.FormatUint(uint64(id), 10)
}

// HandOverEarly sends report to the server named server of the replica that
// took over the repeater id from this one, at the claim, before StopFor:
// what the server here knows then, so that the server there can go on at
// once. HandOver still follows it with the full report.
func (h *Hub) HandOverEarly(server string, id uint, report []byte) {
	if h.pubsub == nil {
		return
	}
	if err := h.pubsub.Publish(earlyHandoverTopic(server, id), report); err != nil {
		slog.Warn("failed to publish an early handover", "repeaterID", id, "error", err)
	}
}

// AwaitHandOverEarly is AwaitHandOver for the report of HandOverEarly.
func (h *Hub) AwaitHandOverEarly(server string, id uint) pubsub.Subscription {
	if h.pubsub == nil {
		return nil
	}
	return h.pubsub.Subscribe(earlyHandoverTopic(server, id))
}

// AwaitHandOver subscribes for the report that the server named server of
// the replica that served the repeater id sends with HandOver once this one
// claims it. Call it before the claim. It returns nil without pubsub.
func (h *Hub) AwaitHandOver(server string, id uint) pubsub.Subscription {
	if h.pubsub == nil {
		return nil
	}
	return h.pubsub.Subscribe(handoverTopic(server, id))
}
