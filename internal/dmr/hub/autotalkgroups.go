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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/gorm/clause"
)

const (
	maxTalkgroupID     = 16777215
	ephemeralSweepMax  = time.Minute
	ephemeralSweepMin  = 50 * time.Millisecond
	unlinkTalkgroupID  = 4000
	autoTalkgroupNamed = "TG "
)

// autoCreateTalkgroup makes the talkgroup of the group voice burst packet
// when it does not exist and the routing settings make unknown
// talkgroups. Only the first burst to a talkgroup that the hub does not
// know waits on the database.
func (h *Hub) autoCreateTalkgroup(packet models.Packet, sourceName string, isVoice bool) {
	if !packet.GroupCall || !isVoice || packet.Dst == 0 || packet.Dst == unlinkTalkgroupID || packet.Dst > maxTalkgroupID {
		return
	}
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return
	}
	if h.db == nil || !h.routingConfig().AutoCreate {
		return
	}
	if exists, err := h.talkgroupExists(packet.Dst); err != nil || exists {
		return
	}
	now := time.Now()
	tg := models.Talkgroup{ID: packet.Dst, Name: autoTalkgroupNamed + strconv.FormatUint(uint64(packet.Dst), 10), AutoCreated: true, LastCallAt: &now}
	res := h.db.Clauses(clause.OnConflict{DoNothing: true}).Create(&tg)
	if res.Error != nil {
		slog.Error("Cannot make a talkgroup for a call to an unknown talkgroup", "talkgroupID", packet.Dst, "error", res.Error)
		return
	}
	if res.RowsAffected > 0 {
		slog.Info("Made a talkgroup for a call to an unknown talkgroup", "talkgroupID", packet.Dst, "src", packet.Src, "repeaterID", packet.Repeater)
	}
	h.TalkgroupChanged(packet.Dst)
}

// noteTalkgroupCall keeps the time of a group voice call on tg, when the
// hub made tg by itself, for the sweep of the ephemeral talkgroups to
// write to the database.
func (h *Hub) noteTalkgroupCall(tg uint) {
	if _, auto := h.autoTalkgroups.Load(tg); auto {
		h.talkgroupCalls.Store(tg, time.Now())
	}
}

func ephemeralSweepEvery(ttl time.Duration) time.Duration {
	if ttl <= 0 {
		return ephemeralSweepMax
	}
	return min(max(ttl/4, ephemeralSweepMin), ephemeralSweepMax)
}

// runEphemeralTalkgroups writes the last calls of the talkgroups that the
// hub made by itself, and deletes the ones with no call for the ephemeral
// TTL, until the hub stops.
func (h *Hub) runEphemeralTalkgroups() {
	timer := time.NewTimer(ephemeralSweepMin)
	defer timer.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-timer.C:
		}
		r := h.routingConfig()
		h.writeTalkgroupCalls()
		if r.Ephemeral && r.EphemeralTTL > 0 {
			h.expireTalkgroups(r.EphemeralTTL)
		}
		timer.Reset(ephemeralSweepEvery(h.routingConfig().EphemeralTTL))
	}
}

func (h *Hub) writeTalkgroupCalls() {
	if h.db == nil {
		return
	}
	h.talkgroupCalls.Range(func(tg uint, at time.Time) bool {
		h.talkgroupCalls.Compute(tg, func(cur time.Time, loaded bool) (time.Time, xsync.ComputeOp) {
			if loaded && !cur.After(at) {
				return cur, xsync.DeleteOp
			}
			return cur, xsync.CancelOp
		})
		if err := h.db.Model(&models.Talkgroup{}).
			Where("id = ? AND auto_created = ? AND (last_call_at IS NULL OR last_call_at < ?)", tg, true, at).
			UpdateColumn("last_call_at", at).Error; err != nil {
			slog.Error("Cannot write the last call of a talkgroup", "talkgroupID", tg, "error", err)
		}
		return true
	})
}

func (h *Hub) expireTalkgroups(ttl time.Duration) {
	if h.db == nil {
		return
	}
	var ids []uint
	if err := h.db.Model(&models.Talkgroup{}).Where("auto_created = ? AND last_call_at < ?", true, time.Now().Add(-ttl)).Pluck("id", &ids).Error; err != nil {
		slog.Error("Cannot list the ephemeral talkgroups", "error", err)
		return
	}
	for _, id := range ids {
		if at, ok := h.talkgroupCalls.Load(id); ok && time.Since(at) < ttl {
			continue
		}
		affected, err := models.DeleteTalkgroup(h.db, id)
		if err != nil {
			slog.Error("Cannot delete an ephemeral talkgroup", "talkgroupID", id, "error", err)
			continue
		}
		slog.Info("Deleted an ephemeral talkgroup with no call for its TTL", "talkgroupID", id, "ttl", ttl)
		for _, rid := range affected {
			h.ReloadRepeater(context.Background(), rid)
		}
		h.TalkgroupChanged(id)
		h.RulesChanged(id)
	}
}
