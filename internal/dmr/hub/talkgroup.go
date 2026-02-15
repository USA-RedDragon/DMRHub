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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"go.opentelemetry.io/otel"
)

// doUnlink handles talkgroup unlinking (TG 4000).
func (h *Hub) doUnlink(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.doUnlink")
	defer span.End()

	dbRepeater, err := models.FindRepeaterByID(h.db, packet.Repeater)
	if err != nil {
		slog.Error("Error finding repeater for unlink", "error", err)
		return
	}

	h.unlinkTimeslot(&dbRepeater, packet.Slot)

	if err := h.db.Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater", "error", err)
	}
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
		h.unsubscribeTalkgroup(repeater.ID, oldTGID, ts)
	}
}

// switchDynamicTalkgroup updates the dynamic talkgroup assignment for a repeater
// when it transmits a group call on a talkgroup it isn't currently linked to.
func (h *Hub) switchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.switchDynamicTalkgroup")
	defer span.End()

	repeater, err := models.FindRepeaterByID(h.db, packet.Repeater)
	if err != nil {
		slog.Error("Error finding repeater", "repeaterID", packet.Repeater, "error", err)
		return
	}

	talkgroup, err := models.FindTalkgroupByID(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error finding talkgroup", "talkgroupID", packet.Dst, "error", err)
		return
	}

	var currentDynTGID **uint
	var dynTG *models.Talkgroup
	var ts int

	if packet.Slot {
		currentDynTGID = &repeater.TS2DynamicTalkgroupID
		dynTG = &repeater.TS2DynamicTalkgroup
		ts = 2
	} else {
		currentDynTGID = &repeater.TS1DynamicTalkgroupID
		dynTG = &repeater.TS1DynamicTalkgroup
		ts = 1
	}

	if *currentDynTGID == nil || **currentDynTGID != packet.Dst {
		slog.Info("Dynamically Linking timeslot", "timeslot", ts, "repeaterID", packet.Repeater, "talkgroupID", packet.Dst)
		*dynTG = talkgroup
		*currentDynTGID = &packet.Dst
		h.subscribeTalkgroup(ctx, repeater.ID, packet.Dst)
		if err := h.db.Save(&repeater).Error; err != nil {
			slog.Error("Error saving repeater", "error", err)
		}
	}
}
