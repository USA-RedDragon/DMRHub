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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
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

type heardAt struct {
	repeater uint
	at       time.Time
}

func (h *Hub) noteHeardOn(packet models.Packet, sourceName string) {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return
	}
	h.heardOn.Store(packet.Src, heardAt{repeater: packet.Repeater, at: time.Now()})
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
			h.recordSighting(s)
		case now := <-ticker.C:
			pruneStreams(h.sightedStreams, now)
			h.heardOn.Range(func(src uint, heard heardAt) bool {
				if now.Sub(heard.at) > sightedStreamTTL {
					h.heardOn.Delete(src)
				}
				return true
			})
			pruneStreams(h.undeliverableLogged, now)
			pruneStreams(h.loopLogged, now)
			pruneStreams(h.streamTalkgroups, now)
			h.talkgroupIDs.Prune(now)
			h.radioOwners.Prune(now)
			h.radioCalls.Prune(now)
			h.egressTable.Prune(now)
			h.repeaters.prune(now)
			h.streamPeers.Range(func(k streamTarget, v streamPeerSet) bool {
				if now.Sub(v.at) > sightedStreamTTL {
					h.streamPeers.Delete(k)
				}
				return true
			})
			h.pruneStreamClaims(now)
			h.pruneSlotBuckets(now)
			h.pruneCodecStreams(now)
			h.streamTargets.Range(func(k streamTarget, v targetAt) bool {
				if now.Sub(v.at) > sightedStreamTTL {
					h.streamTargets.Delete(k)
				}
				return true
			})
		}
	}
}

// recordSighting moves the routing location of a radio that a user owns,
// as the account ID or a radio ID the user added, to the repeater that
// heard it, when that user owns the repeater or an admin trusts it. Other
// sightings are logged only, so a hotspot that spoofs a source ID cannot
// pull that radio's private traffic. The record in the KV store, and the
// word to the plugins, go through the outbox of the radio, so that a
// paused Redis holds no other sighting.
func (h *Hub) recordSighting(s sighting) {
	owner, err := h.radioOwners.Get(s.src)
	if err != nil {
		slog.Error("Error finding the owner of a radio", "radioID", s.src, "error", err)
		return
	}
	if owner == 0 {
		return
	}
	rpt, ok := h.routableFor(owner, s.repeater)
	if !ok && rpt.ID == 0 {
		return
	}
	if !ok {
		slog.Info("Radio heard on a repeater that its user does not own; private routing does not follow",
			"radioID", s.src, "repeaterID", s.repeater, "repeaterOwner", rpt.OwnerID)
		return
	}
	h.tellHeard(s, &rpt)
	key := fmt.Sprintf("%s%d", lastSeenKeyPrefix, s.src)
	value := strconv.FormatUint(uint64(rpt.ID), 10)
	h.later.Add(laterKey{what: sightingsWork, id: s.src}, outbox.Job{
		Topic:      key,
		Repeatable: true,
		Send:       func() error { return h.storeSighting(s.src, key, value) },
	})
}

// storeSighting records in the KV store under key that the radio was
// last heard on the repeater value.
func (h *Hub) storeSighting(radio uint, key, value string) error {
	ctx := context.Background()
	if prev, err := h.kv.Get(ctx, key); err == nil && string(prev) != value {
		slog.Info("Radio moved", "radioID", radio, "from", string(prev), "to", value)
	}
	if err := h.kv.Set(ctx, key, []byte(value)); err != nil {
		return fmt.Errorf("storing where radio %d was heard: %w", radio, err)
	}
	if err := h.kv.Expire(ctx, key, lastSeenTTL); err != nil {
		return fmt.Errorf("expiring where radio %d was heard: %w", radio, err)
	}
	return nil
}

// radioOwner returns the user who owns the radio ID id, as the account ID
// or as a radio ID the user added, and 0 when no user does.
func (h *Hub) radioOwner(id uint) (uint, error) {
	owner, ok, err := models.RadioIDOwner(h.db, id)
	if err != nil || !ok {
		return 0, err //nolint:wrapcheck // RadioIDOwner wraps its errors
	}
	return owner, nil
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
	repeater, cc := rpt.ID, rpt.ColorCode&0xF
	h.later.Add(laterKey{what: sightingsWork, id: s.src}, outbox.Job{
		Topic: plugins.HeardTopic,
		Send:  func() error { return plugins.PublishHeard(h.pubsub, s.src, repeater, slot, cc) },
	})
}

// locateRadio returns the repeater to send private traffic for the radio
// radioID of the user owner to: where it was last heard on a repeater that
// owner owns or an admin trusts. It waits for the KV store at most
// lookup.Wait, as for the database, and then goes on without its answer,
// to where this hub heard the radio and, for an account ID, to its last
// call.
func (h *Hub) locateRadio(ctx context.Context, radioID, owner uint) (uint, bool) {
	key := fmt.Sprintf("%s%d", lastSeenKeyPrefix, radioID)
	read := context.WithoutCancel(ctx)
	if v, err := h.lastSeen.Do(radioID, func() ([]byte, error) { return h.kv.Get(read, key) }); err == nil { //nolint:wrapcheck
		if id, err := strconv.ParseUint(string(v), 10, 32); err == nil {
			if rpt, err := h.routingRepeater(uint(id)); err == nil && (rpt.OwnerID == owner || rpt.Trusted) {
				return uint(id), true
			}
		}
	}
	if heard, ok := h.heardOn.Load(radioID); ok && time.Since(heard.at) < sightedStreamTTL {
		if rpt, err := h.routingRepeater(heard.repeater); err == nil && (rpt.OwnerID == owner || rpt.Trusted) {
			return heard.repeater, true
		}
	}
	if radioID != owner {
		return 0, false
	}
	repeaterID, err := h.radioCalls.Get(radioID)
	if err != nil {
		slog.Error("Error querying where a radio was last heard", "userID", radioID, "error", err)
		return 0, false
	}
	return repeaterID, repeaterID != 0
}

func (h *Hub) lastCallRepeater(userID uint) (uint, error) {
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
		return 0, fmt.Errorf("querying the calls of radio %d: %w", userID, err)
	}
	return repeaterID, nil
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
