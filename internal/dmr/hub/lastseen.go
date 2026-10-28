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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/gorm"
)

const (
	lastHeardKeyPrefix = "dmrhub:lastheard:"
	sightingQueueSize  = 256
	sightedStreamTTL   = 30 * time.Second
)

type sighting struct {
	src, repeater uint
	slot          bool
	at            time.Time
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
	case h.sightings <- sighting{src: packet.Src, repeater: packet.Repeater, slot: packet.Slot, at: time.Now()}:
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
			pruneStreams(h.privateDropLogged, now)
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

// recordSighting keeps where the radio of s was heard, in a record that
// lasts. Private traffic in the anyone reach follows the radio to every
// repeater that heard it. In the hybrid and members reach, a radio that a
// user owns, as the account ID or a radio ID the user added, moves only to
// a repeater that its user owns or an admin trusts; other sightings of it
// are logged only, so a hotspot that spoofs a source ID cannot pull that
// radio's private traffic. The check of the owner, the record in the KV
// store and the word to the plugins go through the outbox of the radio,
// so that a slow database or a paused Redis holds no other sighting, and
// a check that the database does not answer runs again.
func (h *Hub) recordSighting(s sighting) {
	h.later.Add(laterKey{what: sightingsWork, id: s.src}, outbox.Job{
		Topic:      lastHeardKey(s.src),
		Repeatable: true,
		Send:       func() error { return h.vetSighting(s) },
	})
}

// vetSighting checks whether private traffic in the hybrid and members
// reach may follow the radio of s to its repeater, and queues the record
// of s. It fails when the database does not answer.
func (h *Hub) vetSighting(s sighting) error {
	owner, err := h.radioOwners.Get(s.src)
	if err != nil {
		return fmt.Errorf("finding the owner of radio %d: %w", s.src, err)
	}
	vetted := true
	if owner != 0 {
		rpt, ok, err := h.routableFor(owner, s.repeater)
		if err != nil {
			return err
		}
		if !ok && rpt.ID == 0 {
			return nil
		}
		if ok {
			h.tellHeard(s, &rpt)
		} else {
			vetted = false
			slog.Info("Radio heard on a repeater that its user does not own; private routing follows it only in the anyone reach",
				"radioID", s.src, "repeaterID", s.repeater, "repeaterOwner", rpt.OwnerID)
		}
	}
	key := lastHeardKey(s.src)
	h.later.Add(laterKey{what: sightingsWork, id: s.src}, outbox.Job{
		Topic:      key,
		Repeatable: true,
		Send:       func() error { return h.storeSighting(s, key, vetted) },
	})
	return nil
}

func lastHeardKey(radio uint) string {
	return lastHeardKeyPrefix + strconv.FormatUint(uint64(radio), 10)
}

// lastHeard is the record in the KV store of where a radio was last
// heard. Vetted is the last repeater that private traffic in the hybrid
// and members reach may follow the radio to: for a radio that a user
// owns, a repeater that the user owns or an admin trusts, and for other
// radios any repeater. Any is the last repeater that heard the radio.
type lastHeard struct {
	Vetted heardRecord `json:"vetted"`
	Any    heardRecord `json:"any"`
}

// heardRecord is a repeater and the time in Unix milliseconds that it
// heard a radio.
type heardRecord struct {
	Repeater uint  `json:"repeater,omitempty"`
	At       int64 `json:"at,omitempty"`
}

func (r heardRecord) heard() heardAt {
	if r.Repeater == 0 {
		return heardAt{}
	}
	return heardAt{repeater: r.Repeater, at: time.UnixMilli(r.At)}
}

// storeSighting adds the sighting s to the record under key in the KV
// store. The newest sighting wins, so a sighting that the outbox sends
// again after a newer one changes nothing.
func (h *Hub) storeSighting(s sighting, key string, vetted bool) error {
	ctx := context.Background()
	var rec lastHeard
	prev, err := h.kv.Get(ctx, key)
	switch {
	case err == nil:
		if err := json.Unmarshal(prev, &rec); err != nil {
			rec = lastHeard{}
		}
	case !errors.Is(err, kv.ErrNotFound):
		return fmt.Errorf("reading where radio %d was heard: %w", s.src, err)
	}
	at := s.at.UnixMilli()
	changed := false
	if at > rec.Any.At {
		rec.Any = heardRecord{Repeater: s.repeater, At: at}
		changed = true
	}
	if vetted && at > rec.Vetted.At {
		if rec.Vetted.Repeater != 0 && rec.Vetted.Repeater != s.repeater {
			slog.Info("Radio moved", "radioID", s.src, "from", rec.Vetted.Repeater, "to", s.repeater)
		}
		rec.Vetted = heardRecord{Repeater: s.repeater, At: at}
		changed = true
	}
	if !changed {
		return nil
	}
	value, err := json.Marshal(rec)
	if err != nil {
		return fmt.Errorf("encoding where radio %d was heard: %w", s.src, err)
	}
	if err := h.kv.Set(ctx, key, value); err != nil {
		return fmt.Errorf("storing where radio %d was heard: %w", s.src, err)
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

// locateRadio returns the repeater to send private traffic under route
// for the radio radioID, which the user owner owns, or no user when owner
// is 0: where the radio was heard, within the max age of route, on a
// repeater that the reach of route lets the traffic follow it to. It takes
// the first of the lasting record in the KV store, for which it waits at
// most lookup.Wait, as for the database; where this hub heard the radio
// in the last seconds, which the record may not hold yet; and, for an
// account ID, its last call on a repeater that the user owns or an admin
// trusts.
func (h *Hub) locateRadio(ctx context.Context, radioID, owner uint, route config.PrivateRoute) (uint, bool) {
	anyone := route.Reach == config.PrivateReachAnyone
	now := time.Now()
	var best heardAt
	consider := func(c heardAt) {
		if c.repeater == 0 || !c.at.After(best.at) || (route.MaxAge > 0 && now.Sub(c.at) > route.MaxAge) {
			return
		}
		rpt, err := h.routingRepeater(c.repeater)
		if err != nil || (!anyone && owner != 0 && rpt.OwnerID != owner && !rpt.Trusted) {
			return
		}
		best = c
	}
	key := lastHeardKey(radioID)
	read := context.WithoutCancel(ctx)
	if v, err := h.lastSeen.Do(radioID, func() ([]byte, error) { return h.kv.Get(read, key) }); err == nil { //nolint:wrapcheck
		var rec lastHeard
		if json.Unmarshal(v, &rec) == nil {
			if anyone {
				consider(rec.Any.heard())
			} else {
				consider(rec.Vetted.heard())
			}
		}
	}
	if heard, ok := h.heardOn.Load(radioID); best.repeater == 0 && ok && time.Since(heard.at) < sightedStreamTTL {
		consider(heard)
	}
	if best.repeater == 0 && radioID == owner {
		call, err := h.radioCalls.Get(radioID)
		if err != nil {
			slog.Error("Error querying where a radio was last heard", "userID", radioID, "error", err)
		} else {
			consider(call)
		}
	}
	return best.repeater, best.repeater != 0
}

// lastCallRepeater returns the repeater and time of the last call of the
// user userID on a repeater that the user owns or an admin trusts.
func (h *Hub) lastCallRepeater(userID uint) (heardAt, error) {
	var row struct {
		RepeaterID uint
		CreatedAt  time.Time
	}
	err := h.db.Table("calls").
		Select("calls.repeater_id, calls.created_at").
		Joins("JOIN repeaters ON repeaters.id = calls.repeater_id AND repeaters.deleted_at IS NULL").
		Where("calls.user_id = ? AND calls.deleted_at IS NULL", userID).
		Where("repeaters.owner_id = ? OR repeaters.trusted = ?", userID, true).
		Order("calls.created_at DESC").
		Limit(1).
		Scan(&row).Error
	if err != nil {
		return heardAt{}, fmt.Errorf("querying the calls of radio %d: %w", userID, err)
	}
	return heardAt{repeater: row.RepeaterID, at: row.CreatedAt}, nil
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
func (h *Hub) routableFor(userID, repeaterID uint) (models.Repeater, bool, error) {
	var rpt models.Repeater
	err := h.db.Select("id", "owner_id", "trusted", "color_code").First(&rpt, repeaterID).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return models.Repeater{}, false, nil
	}
	if err != nil {
		return models.Repeater{}, false, fmt.Errorf("finding repeater %d: %w", repeaterID, err)
	}
	return rpt, rpt.OwnerID == userID || rpt.Trusted, nil
}
