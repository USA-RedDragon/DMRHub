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
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
)

// claimedTopic carries each claim of a repeater by a replica, as
// "<repeater ID> <instance ID> <since>", where since is the time in Unix
// nanoseconds when the packet that made the replica claim it came in.
const claimedTopic = "hub:claimed"

func ownerKey(repeaterID uint) string {
	return fmt.Sprintf("%s%d", peerOwnerKeyPrefix, repeaterID)
}

// InstanceID returns the ID of the replica of this hub, or "" when it has
// none.
func (h *Hub) InstanceID() string {
	return h.instanceID
}

// owner returns the replica that last claimed the repeater.
func (h *Hub) owner(ctx context.Context, repeaterID uint) (string, bool) {
	if h.kv == nil {
		return "", false
	}
	v, err := h.kv.Get(ctx, ownerKey(repeaterID))
	if err != nil || len(v) == 0 {
		return "", false
	}
	return string(v), true
}

// moveTTL is how long the record of a takeover of a repeater from one
// owner lasts.
const moveTTL = 2 * time.Minute

// OwnerLeaving reports whether the repeater was last claimed by another
// replica that drains, so that this replica may take its session over. It
// is false without replica IDs, as for a lone instance, when the owner
// still serves, and after the owner stopped or died.
func (h *Hub) OwnerLeaving(ctx context.Context, repeaterID uint) bool {
	_, ok := h.leavingOwner(ctx, repeaterID)
	return ok
}

func (h *Hub) leavingOwner(ctx context.Context, repeaterID uint) (string, bool) {
	if h.instanceID == "" {
		return "", false
	}
	owner, ok := h.owner(ctx, repeaterID)
	if !ok || owner == h.instanceID {
		return "", false
	}
	return owner, servers.InstanceDraining(ctx, h.kv, owner)
}

// TakeOver reports whether this replica may take over the session of the
// repeater from its owner, which drains, as OwnerLeaving does. Of the
// replicas that ask about one owner, only the first gets true, so that
// two addresses that claim the repeater at two replicas at once never
// both get its session.
func (h *Hub) TakeOver(ctx context.Context, repeaterID uint) bool {
	owner, ok := h.leavingOwner(ctx, repeaterID)
	if !ok {
		return false
	}
	key := fmt.Sprintf("dmrhub:move:%d:%s", repeaterID, owner)
	won, err := h.kv.SetNX(ctx, key, h.instanceID, moveTTL)
	if err != nil {
		slog.Warn("failed to record a takeover of a repeater", "repeaterID", repeaterID, "error", err)
		return false
	}
	return won
}

// OnClaimedElsewhere calls fn with each repeater that is active on this
// hub when another replica claims it, and the time when the packet that
// made the other replica claim it came in there. A server ends its own
// session of the repeater then, without a word to the repeater, which
// another replica serves now. The returned func stops the calls.
func (h *Hub) OnClaimedElsewhere(fn func(repeaterID uint, since time.Time)) (stop func()) {
	h.claimedMu.Lock()
	defer h.claimedMu.Unlock()
	if h.claimed == nil {
		h.claimed = map[int]func(uint, time.Time){}
	}
	h.claimedNext++
	n := h.claimedNext
	h.claimed[n] = fn
	return func() {
		h.claimedMu.Lock()
		defer h.claimedMu.Unlock()
		delete(h.claimed, n)
	}
}

func (h *Hub) followClaims(sub pubsub.Subscription) {
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	for {
		select {
		case <-h.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			id, instance, since, ok := parseClaim(msg)
			if !ok || instance == h.instanceID {
				continue
			}
			if _, active := h.subscriptionMgr.subscriptions.Load(id); !active {
				continue
			}
			h.claimedMu.Lock()
			fns := make([]func(uint, time.Time), 0, len(h.claimed))
			for _, fn := range h.claimed {
				fns = append(fns, fn)
			}
			h.claimedMu.Unlock()
			for _, fn := range fns {
				h.handing.Add()
				go func() {
					defer h.handing.Done()
					fn(id, since)
				}()
			}
		}
	}
}

func (h *Hub) publishClaim(repeaterID uint, since time.Time) {
	if h.pubsub == nil {
		return
	}
	msg := strconv.FormatUint(uint64(repeaterID), 10) + " " + h.instanceID + " " + strconv.FormatInt(since.UnixNano(), 10)
	if err := h.pubsub.Publish(claimedTopic, []byte(msg)); err != nil {
		slog.Warn("failed to publish a repeater claim", "repeaterID", repeaterID, "error", err)
	}
}

func parseClaim(b []byte) (uint, string, time.Time, bool) {
	f := strings.Fields(string(b))
	if len(f) < 2 {
		return 0, "", time.Time{}, false
	}
	n, err := strconv.ParseUint(f[0], 10, 64)
	if err != nil {
		return 0, "", time.Time{}, false
	}
	since := time.Now()
	if len(f) > 2 {
		if ns, err := strconv.ParseInt(f[2], 10, 64); err == nil {
			since = time.Unix(0, ns)
		}
	}
	return uint(n), f[1], since, true
}

func (h *Hub) activeRepeaters() map[uint]bool {
	out := map[uint]bool{}
	h.subscriptionMgr.subscriptions.Range(func(id uint, _ *xsync.Map[uint, *subscription]) bool {
		out[id] = true
		return true
	})
	return out
}

// WaitMoved blocks until another replica has claimed every repeater that
// is active on this hub, or a repeater has left this hub, or ctx ends. It
// returns the number of repeaters that are still here. A draining replica
// calls it before it stops, so that it keeps serving each repeater until
// the repeater's next packet has reached another replica.
func (h *Hub) WaitMoved(ctx context.Context) int {
	if h.instanceID == "" || h.pubsub == nil {
		return len(h.activeRepeaters())
	}
	sub := h.pubsub.Subscribe(claimedTopic)
	defer func() { _ = sub.Close() }()
	pending := h.activeRepeaters()
	h.dropClaimed(ctx, pending)
	recheck := time.NewTicker(claimRecheck)
	defer recheck.Stop()
	ch := sub.Channel()
	for len(pending) > 0 {
		select {
		case <-ctx.Done():
			return len(pending)
		case <-recheck.C:
			h.dropClaimed(ctx, pending)
		case <-h.left:
		case msg, ok := <-ch:
			if !ok {
				return len(pending)
			}
			if id, instance, _, ok := parseClaim(msg); ok && instance != h.instanceID {
				delete(pending, id)
			}
		}
		active := h.activeRepeaters()
		for id := range pending {
			if !active[id] {
				delete(pending, id)
			}
		}
	}
	h.waitHanding(ctx)
	return 0
}

// waitHanding waits until the servers handed over every repeater that
// another replica claimed, or ctx ends.
func (h *Hub) waitHanding(ctx context.Context) {
	done := make(chan struct{})
	go func() {
		h.handing.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

// claimRecheck is how often WaitMoved reads the owners again, in case it
// missed a claim on pubsub.
const claimRecheck = time.Second

func (h *Hub) dropClaimed(ctx context.Context, pending map[uint]bool) {
	for id := range pending {
		if owner, ok := h.owner(ctx, id); ok && owner != h.instanceID {
			delete(pending, id)
		}
	}
}
