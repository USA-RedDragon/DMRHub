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
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/puzpuzpuz/xsync/v4"
)

type heldIngress struct {
	ctx      context.Context //nolint:containedctx
	packet   models.Packet
	at       time.Time
	admitted func(bool)
}

type ingressHold struct {
	mu   sync.Mutex
	held []heldIngress
	done bool
	once sync.Once
}

// HoldIngress holds the bursts that the server named server takes from the
// repeater id, which moves here from another replica, until release is
// called or the HandOverWait of wait and limit ends. Then it routes them
// in the order they came, before the bursts that come after them. Call it
// before the claim of the repeater, and call release when the handover of
// the replica that served the repeater comes: that replica sends it after
// ReleaseIngress, so the bursts that come here go out after the bursts
// that came there.
func (h *Hub) HoldIngress(ctx context.Context, server string, id uint, wait, limit time.Duration) (release func()) {
	src := streamSource{server: server, repeater: id}
	hold := &ingressHold{}
	if old, loaded := h.ingressHolds.LoadOrStore(src, hold); loaded {
		hold = old
	} else {
		h.ingressHeld.Add(1)
	}
	expired, stop := h.HandOverWait(ctx, id, wait, limit)
	release = func() {
		hold.once.Do(func() {
			stop()
			h.releaseIngress(src, hold)
		})
	}
	h.goTracked(func() {
		<-expired
		release()
	})
	return release
}

func (h *Hub) holdIngress(ctx context.Context, packet models.Packet, sourceName string, at time.Time, admitted func(bool)) bool {
	if h.ingressHeld.Load() == 0 {
		return false
	}
	hold, ok := h.ingressHolds.Load(streamSource{server: sourceName, repeater: packet.Repeater})
	if !ok {
		return false
	}
	hold.mu.Lock()
	defer hold.mu.Unlock()
	if hold.done {
		return false
	}
	hold.held = append(hold.held, heldIngress{ctx: ctx, packet: packet, at: at, admitted: admitted})
	return true
}

func (h *Hub) releaseIngress(src streamSource, hold *ingressHold) {
	for {
		hold.mu.Lock()
		if len(hold.held) == 0 {
			hold.done = true
			hold.mu.Unlock()
			h.ingressHolds.Compute(src, func(cur *ingressHold, loaded bool) (*ingressHold, xsync.ComputeOp) {
				if loaded && cur == hold {
					h.ingressHeld.Add(-1)
					return nil, xsync.DeleteOp
				}
				return cur, xsync.CancelOp
			})
			return
		}
		batch := hold.held
		hold.held = nil
		hold.mu.Unlock()
		for _, b := range batch {
			h.routeStreamAt(b.ctx, b.packet, src.server, b.at, b.admitted)
		}
	}
}

// ReleaseIngress returns true once every burst that the server named
// server took from the repeater id before the call went out from this
// replica: the hub routed it and its copy in the other codec, published
// it to the repeaters, and each peer server took it, which for OpenBridge
// means it sent it or the replica that sends OpenBridge egress did. A
// server that hands a repeater over to another replica stops taking its
// bursts, calls it, and then says in its handover whether it returned
// true, so that the other replica holds the bursts that it takes until
// then. It returns false when limit passes or the hub stops first.
func (h *Hub) ReleaseIngress(server string, id uint, limit time.Duration) bool {
	quit := make(chan struct{})
	defer close(quit)
	done := make(chan bool, 1)
	h.goTracked(func() { done <- h.drainIngress(server, id, quit) })
	t := h.clock.NewTimer(limit)
	defer t.Stop()
	select {
	case ok := <-done:
		return ok
	case <-t.C():
		slog.Warn("The bursts of a repeater that moved away did not go out in time", "server", server, "repeaterID", id)
		return false
	case <-h.done:
		return false
	}
}

func (h *Hub) drainIngress(server string, id uint, quit <-chan struct{}) bool {
	for _, slot := range []bool{false, true} {
		key := sourceSlot{streamSource: streamSource{server: server, repeater: id}, slot: slot}
		if !h.waitClaimWait(key, quit) || !waitQueue(h.routing, key, quit) || !h.waitCopies(key, quit) ||
			!waitQueue(h.peerEgress, key, quit) || !h.waitOutbox(key, quit) {
			return false
		}
	}
	for _, name := range h.peerServers() {
		if !h.barrierUnless(name, quit) {
			return false
		}
	}
	return true
}

func (h *Hub) waitClaimWait(key sourceSlot, quit <-chan struct{}) bool {
	for {
		w, ok := h.claimWaits.Load(key)
		if !ok {
			return true
		}
		select {
		case <-w.drained:
		case <-quit:
			return false
		}
	}
}

func waitQueue(q *streamorder.Queue[sourceSlot], key sourceSlot, quit <-chan struct{}) bool {
	done := make(chan struct{})
	if !q.Do(context.Background(), key, func() { close(done) }) {
		return false
	}
	select {
	case <-done:
		return true
	case <-quit:
		return false
	}
}

func (h *Hub) waitCopies(key sourceSlot, quit <-chan struct{}) bool {
	h.copyMu.Lock()
	o := h.copyOrders[key]
	if o == nil || len(o.queue) == 0 {
		h.copyMu.Unlock()
		return true
	}
	done := make(chan struct{})
	o.queue = append(o.queue, &copyTicket{key: key, done: true, then: done})
	h.copyMu.Unlock()
	select {
	case <-done:
		return true
	case <-quit:
		return false
	}
}

func (h *Hub) waitOutbox(key sourceSlot, quit <-chan struct{}) bool {
	done := make(chan struct{})
	h.outbox.Add(key, outbox.Job{Topic: "ingress release", Send: func() error {
		close(done)
		return nil
	}})
	select {
	case <-done:
		return true
	case <-quit:
		return false
	}
}
