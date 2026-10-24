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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	lastSeenKeyPrefix = "dmrhub:lastseen:"
	lastSeenTTL       = 15 * time.Minute
	sightingQueueSize = 256
	sightedStreamTTL  = 30 * time.Second
)

type sighting struct {
	src, repeater uint
	slot          bool
}

type sightingKey struct {
	src, stream uint
}

// noteSighting queues the place where the source of packet was heard, once
// per stream. It never blocks.
func (h *Hub) noteSighting(packet models.Packet, sourceName string) {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return
	}
	if _, seen := h.sightedStreams.LoadOrStore(sightingKey{src: packet.Src, stream: packet.StreamID}, time.Now()); seen {
		return
	}
	select {
	case h.sightings <- sighting{src: packet.Src, repeater: packet.Repeater, slot: packet.Slot}:
	default:
		slog.Warn("Dropping a sighting, the queue is full", "src", packet.Src, "repeater", packet.Repeater)
	}
}

func (h *Hub) runSightings() {
	ticker := time.NewTicker(sightedStreamTTL)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case s := <-h.sightings:
			h.recordSighting(context.Background(), s)
		case now := <-ticker.C:
			pruneStreams(h.sightedStreams, now)
			pruneStreams(h.undeliverableLogged, now)
			pruneStreams(h.parrotEnded, now)
			pruneStreams(h.loopLogged, now)
			pruneStreams(h.streamTalkgroups, now)
			h.streamPeers.Range(func(k streamTarget, v streamPeerSet) bool {
				if now.Sub(v.at) > sightedStreamTTL {
					h.streamPeers.Delete(k)
				}
				return true
			})
			h.pruneStreamClaims(now)
			h.pruneSlotBuckets(now)
			h.streamTargets.Range(func(k streamTarget, v targetAt) bool {
				if now.Sub(v.at) > sightedStreamTTL {
					h.streamTargets.Delete(k)
				}
				return true
			})
		}
	}
}

// recordSighting moves the routing location of a registered radio to the
// repeater that heard it, when the radio's user owns that repeater or an
// admin trusts it. Other sightings are logged only, so a hotspot that
// spoofs a source ID cannot pull that radio's private traffic.
func (h *Hub) recordSighting(ctx context.Context, s sighting) {
	user, err := models.FindUserByID(h.db, s.src)
	if err != nil {
		h.noteHeardRadio(s)
		return
	}
	rpt, ok := h.routableFor(user.ID, s.repeater)
	if !ok && rpt.ID == 0 {
		return
	}
	if !ok {
		slog.Info("Radio heard on a repeater that its user does not own; private routing does not follow",
			"radioID", s.src, "repeaterID", s.repeater, "repeaterOwner", rpt.OwnerID)
		return
	}
	h.locator.Heard(s.src, rpt.ID, s.slot)
	h.tellHeard(s, &rpt)
	key := fmt.Sprintf("%s%d", lastSeenKeyPrefix, user.ID)
	if prev, err := h.kv.Get(ctx, key); err == nil && string(prev) != strconv.FormatUint(uint64(rpt.ID), 10) {
		slog.Info("Radio moved", "radioID", user.ID, "from", string(prev), "to", rpt.ID)
	}
	if err := h.kv.Set(ctx, key, []byte(strconv.FormatUint(uint64(rpt.ID), 10))); err != nil {
		slog.Warn("Failed to store where a radio was heard", "radioID", user.ID, "error", err)
		return
	}
	if err := h.kv.Expire(ctx, key, lastSeenTTL); err != nil {
		slog.Warn("Failed to expire where a radio was heard", "radioID", user.ID, "error", err)
	}
}

// noteHeardRadio tells the location server where a radio ID that a user
// added was heard, on the same terms as a sighting of an account ID.
func (h *Hub) noteHeardRadio(s sighting) {
	owner, ok, err := models.RadioIDOwner(h.db, s.src)
	if err != nil || !ok {
		return
	}
	if rpt, ok := h.routableFor(owner, s.repeater); ok {
		h.locator.Heard(s.src, rpt.ID, s.slot)
		h.tellHeard(s, &rpt)
	}
}

// tellHeard tells the plugins that the radio of s was heard on rpt, when
// plugins can connect.
func (h *Hub) tellHeard(s sighting, rpt *models.Repeater) {
	if h.plugins.Load() == nil || h.pubsub == nil {
		return
	}
	slot := uint8(1)
	if s.slot {
		slot = 2
	}
	if err := plugins.PublishHeard(h.pubsub, s.src, rpt.ID, slot, rpt.ColorCode&0xF); err != nil {
		slog.Warn("Failed to tell the plugins where a radio was heard", "radioID", s.src, "error", err)
	}
}

// locateRadio returns the repeater to send private traffic for the
// registered radio userID to: where it was last heard on a repeater that
// its user owns or an admin trusts.
func (h *Hub) locateRadio(ctx context.Context, userID uint) (uint, bool) {
	if v, err := h.kv.Get(ctx, fmt.Sprintf("%s%d", lastSeenKeyPrefix, userID)); err == nil {
		if id, err := strconv.ParseUint(string(v), 10, 32); err == nil {
			if _, ok := h.routableFor(userID, uint(id)); ok {
				return uint(id), true
			}
		}
	}
	var repeaterID uint
	err := h.db.Table("calls").
		Select("calls.repeater_id").
		Joins("JOIN repeaters ON repeaters.id = calls.repeater_id AND repeaters.deleted_at IS NULL").
		Where("calls.user_id = ? AND calls.deleted_at IS NULL", userID).
		Where("repeaters.owner_id = ? OR repeaters.trusted = ?", userID, true).
		Order("calls.created_at DESC").
		Limit(1).
		Scan(&repeaterID).Error
	if err != nil {
		slog.Error("Error querying where a radio was last heard", "userID", userID, "error", err)
		return 0, false
	}
	return repeaterID, repeaterID != 0
}

func pruneStreams[K comparable](m *xsync.Map[K, time.Time], now time.Time) {
	m.Range(func(stream K, at time.Time) bool {
		if now.Sub(at) > sightedStreamTTL {
			m.Delete(stream)
		}
		return true
	})
}

// routableFor reports whether private traffic for the radio of userID may
// follow it to repeaterID: the user owns the repeater or an admin trusts
// it. It returns the repeater it found, with ID 0 when there is none.
func (h *Hub) routableFor(userID, repeaterID uint) (models.Repeater, bool) {
	var rpt models.Repeater
	if err := h.db.Select("id", "owner_id", "trusted", "color_code").First(&rpt, repeaterID).Error; err != nil {
		return models.Repeater{}, false
	}
	return rpt, rpt.OwnerID == userID || rpt.Trusted
}
