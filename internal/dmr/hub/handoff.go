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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// barrierWait is the longest time Barrier waits for a server.
const barrierWait = time.Second

// handoverSettle is how long a replica that hands a repeater over keeps
// its subscriptions of the repeater after the claim, so that it gets each
// packet that was published before the subscriptions of the replica that
// claimed it, which are on other connections to Redis, took effect.
const handoverSettle = 50 * time.Millisecond

// StopFor stops the subscriptions of the repeater id on this hub, which
// another replica claimed, once they had handoverSettle to take the
// packets published before that replica subscribed, and returns once the
// server named server has taken every packet that they handed it. A server
// that hands the repeater over then knows each packet that it got for it.
func (h *Hub) StopFor(server string, id uint) {
	t := time.NewTimer(handoverSettle)
	select {
	case <-t.C:
	case <-h.done:
		t.Stop()
	}
	h.deactivateRepeater(context.Background(), id)
	h.Barrier(server)
}

// Barrier returns once the server named server has taken every packet that
// the hub handed it before the call, or after barrierWait. A server that
// hands a repeater over stops the subscriptions of the repeater and then
// calls it, so that it knows every packet that it got for the repeater.
func (h *Hub) Barrier(server string) {
	h.mu.RLock()
	_, ok := h.servers[server]
	h.mu.RUnlock()
	if !ok {
		return
	}
	done := make(chan struct{})
	go h.deliverToServerUnless(h.done, server, RoutedPacket{Barrier: done})
	t := time.NewTimer(barrierWait)
	defer t.Stop()
	select {
	case <-done:
	case <-t.C:
		slog.Warn("A server did not reach a barrier in time", "server", server)
	case <-h.done:
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
