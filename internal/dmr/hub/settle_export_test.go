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
	"errors"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
)

// KnowTalkgroups waits until the hub has the answer of the database to
// whether each talkgroup in the database exists, so that a burst to one
// does not wait on the database, and returns the error of the database.
func (h *Hub) KnowTalkgroups() error {
	var ids []uint
	if err := h.db.Model(&models.Talkgroup{}).Pluck("id", &ids).Error; err != nil {
		return err
	}
	for _, id := range ids {
		for {
			_, err := h.talkgroupIDs.Get(id)
			if err == nil {
				break
			}
			if !errors.Is(err, lookup.ErrSlow) {
				return fmt.Errorf("knowing talkgroup %d: %w", id, err)
			}
		}
	}
	return nil
}

// RepeaterCacheTTL is repeaterCacheTTL.
const RepeaterCacheTTL = repeaterCacheTTL

// StreamIdle is streamIdle.
const StreamIdle = streamIdle

// KnowPeers waits until the hub has the peers that it sends bursts to from
// the database, so that a burst does not wait on the database.
func (h *Hub) KnowPeers() error {
	for {
		_, err := h.egressTable.Get(struct{}{})
		if err == nil {
			return nil
		}
		if !errors.Is(err, lookup.ErrSlow) {
			return fmt.Errorf("knowing the peers: %w", err)
		}
	}
}

// KnowRepeater waits until the hub has the repeater id in its cache, so
// that a burst to it does not wait on the database.
func (h *Hub) KnowRepeater(id uint) error {
	_, err := h.waitRepeater(id)
	return err
}

// Subscribes reports whether the active repeater repeaterID takes the
// bursts of the talkgroup tg.
func (h *Hub) Subscribes(repeaterID, tg uint) bool {
	subs, ok := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !ok {
		return false
	}
	_, ok = subs.Load(tg)
	return ok
}

// Settled returns a channel that is closed when the hub has routed the
// bursts that came before from the source slot of packet from sourceName,
// made the talkgroup link changes that they asked for, and published them.
func (h *Hub) Settled(packet models.Packet, sourceName string) <-chan struct{} {
	ctx := context.Background()
	key := h.sourceSlotOf(packet, sourceName)
	packet.StreamID = 0
	done := make(chan struct{})
	h.admitStream(ctx, packet, sourceName, func(bool) {
		h.routing.Do(ctx, key, func() {
			linked := make(chan struct{})
			h.links.Do(ctx, packet.Repeater, func() { close(linked) })
			<-linked
			h.outbox.Add(key, outbox.Job{Send: func() error {
				close(done)
				return nil
			}})
		})
	})
	return done
}
