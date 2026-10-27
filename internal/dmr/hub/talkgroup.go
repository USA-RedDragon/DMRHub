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
	"maps"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"go.opentelemetry.io/otel"
)

func (h *Hub) queueLink(ctx context.Context, repeaterID uint, fn func()) {
	if !h.links.Do(ctx, repeaterID, fn) && ctx.Err() == nil {
		slog.Warn("Dropping a talkgroup link change of a backed-up repeater", "repeaterID", repeaterID)
	}
}

// doUnlink handles talkgroup unlinking (TG 4000).
func (h *Hub) doUnlink(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.doUnlink")
	defer span.End()

	h.UnlinkDynamicTalkgroup(ctx, packet.Repeater, packet.Slot)
}

// UnlinkDynamicTalkgroup clears the dynamic talkgroup of a timeslot of the
// repeater, as a voice call to TG 4000 on that timeslot does. slot true is
// TS2.
func (h *Hub) UnlinkDynamicTalkgroup(_ context.Context, repeaterID uint, slot bool) {
	dbRepeater, err := models.FindRepeaterByID(h.db, repeaterID)
	if err != nil {
		slog.Error("Error finding repeater for unlink", "error", err)
		return
	}

	h.unlinkTimeslot(&dbRepeater, slot)
}

// unlinkTimeslot clears the dynamic talkgroup for the given timeslot on a repeater.
// slot=true means TS2, slot=false means TS1.
func (h *Hub) unlinkTimeslot(repeater *models.Repeater, slot bool) {
	var dynamicTGID **uint
	var dynamicTG *models.Talkgroup
	var tsField string
	var assocName string
	var ts dmrconst.Timeslot

	if slot {
		dynamicTGID = &repeater.TS2DynamicTalkgroupID
		dynamicTG = &repeater.TS2DynamicTalkgroup
		tsField = "TS2DynamicTalkgroupID"
		assocName = "TS2DynamicTalkgroup"
		ts = dmrconst.TimeslotTwo
	} else {
		dynamicTGID = &repeater.TS1DynamicTalkgroupID
		dynamicTG = &repeater.TS1DynamicTalkgroup
		tsField = "TS1DynamicTalkgroupID"
		assocName = "TS1DynamicTalkgroup"
		ts = dmrconst.TimeslotOne
	}

	slog.Info("Unlinking timeslot", "timeslot", ts, "repeaterID", repeater.ID)
	if *dynamicTGID != nil {
		oldTGID := **dynamicTGID
		h.db.Model(repeater).Select(tsField).Updates(map[string]interface{}{tsField: nil})
		err := h.db.Model(repeater).Association(assocName).Delete(dynamicTG)
		if err != nil {
			slog.Error("Error deleting dynamic talkgroup association", "association", assocName, "error", err)
		}
		h.repeaterChanged(repeater.ID)
		h.unsubscribeTalkgroup(repeater.ID, oldTGID, ts)
	}
}

// switchDynamicTalkgroup updates the dynamic talkgroup assignment for a repeater
// when it transmits a group call on a talkgroup it isn't currently linked to.
func (h *Hub) switchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.switchDynamicTalkgroup")
	defer span.End()

	h.LinkDynamicTalkgroup(ctx, packet.Repeater, packet.Dst, packet.Slot)
}

// LinkDynamicTalkgroup links the talkgroup as the dynamic talkgroup of a
// timeslot of the repeater, as a voice call on the talkgroup on that
// timeslot does. slot true is TS2.
func (h *Hub) LinkDynamicTalkgroup(ctx context.Context, repeaterID uint, talkgroupID uint, slot bool) {
	repeater, err := h.waitRepeater(repeaterID)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", repeaterID, "error", err)
		return
	}

	currentDynTGID, column, ts := repeater.TS1DynamicTalkgroupID, "ts1_dynamic_talkgroup_id", 1
	if slot {
		currentDynTGID, column, ts = repeater.TS2DynamicTalkgroupID, "ts2_dynamic_talkgroup_id", 2
	}
	if currentDynTGID != nil && *currentDynTGID == talkgroupID {
		return
	}

	talkgroup, err := models.FindTalkgroupByID(h.db, talkgroupID)
	if err != nil {
		slog.Error("Error finding talkgroup", "talkgroupID", talkgroupID, "error", err)
		return
	}

	slog.Info("Dynamically Linking timeslot", "timeslot", ts, "repeaterID", repeaterID, "talkgroupID", talkgroupID)
	if err := h.db.Model(&models.Repeater{}).Where("id = ?", repeater.ID).UpdateColumn(column, talkgroup.ID).Error; err != nil {
		slog.Error("Error saving repeater", "error", err)
		return
	}
	h.repeaterChanged(repeater.ID)
	h.subscribeTalkgroup(ctx, repeater.ID, talkgroupID)
}

// loadTalkgroups reads the IDs of all talkgroups from the database. When
// the database fails, the hub keeps the IDs that it had.
func (h *Hub) loadTalkgroups() {
	var ids []uint
	if err := h.db.Model(&models.Talkgroup{}).Pluck("id", &ids).Error; err != nil {
		slog.Error("Cannot load the talkgroups, keeping the ones known", "error", err)
		return
	}
	set := make(map[uint]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	h.talkgroupsMu.Lock()
	h.talkgroups.Store(&set)
	h.talkgroupsMu.Unlock()
}

// noteTalkgroup reads the talkgroup id again after a change of it.
func (h *Hub) noteTalkgroup(id uint) {
	exists, err := models.TalkgroupIDExists(h.db, id)
	if err != nil {
		slog.Error("Cannot read a changed talkgroup, forgetting it", "talkgroupID", id, "error", err)
		h.talkgroupIDs.Drop(id)
		return
	}
	h.talkgroupIDs.Put(id, exists)
	h.talkgroupsMu.Lock()
	defer h.talkgroupsMu.Unlock()
	set := map[uint]struct{}{}
	if old := h.talkgroups.Load(); old != nil {
		set = maps.Clone(*old)
	}
	if exists {
		set[id] = struct{}{}
	} else {
		delete(set, id)
	}
	h.talkgroups.Store(&set)
}

// talkgroupExists reports whether the talkgroup id exists. It waits on the
// database at most lookup.Wait, and only for a talkgroup that the hub did
// not load.
func (h *Hub) talkgroupExists(id uint) (bool, error) {
	if set := h.talkgroups.Load(); set != nil {
		if _, ok := (*set)[id]; ok {
			return true, nil
		}
	}
	exists, err := h.talkgroupIDs.Get(id)
	if err != nil {
		return false, fmt.Errorf("looking up talkgroup %d: %w", id, err)
	}
	return exists, nil
}
