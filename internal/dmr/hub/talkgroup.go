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

	if packet.Slot {
		slog.Info("Unlinking timeslot 2", "repeaterID", packet.Repeater)
		if dbRepeater.TS2DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS2DynamicTalkgroupID
			h.db.Model(&dbRepeater).Select("TS2DynamicTalkgroupID").Updates(map[string]interface{}{"TS2DynamicTalkgroupID": nil})
			err := h.db.Model(&dbRepeater).Association("TS2DynamicTalkgroup").Delete(&dbRepeater.TS2DynamicTalkgroup)
			if err != nil {
				slog.Error("Error deleting TS2DynamicTalkgroup", "error", err)
			}
			h.unsubscribeTalkgroup(dbRepeater.ID, oldTGID, dmrconst.TimeslotTwo)
		}
	} else {
		slog.Info("Unlinking timeslot 1", "repeaterID", packet.Repeater)
		if dbRepeater.TS1DynamicTalkgroupID != nil {
			oldTGID := *dbRepeater.TS1DynamicTalkgroupID
			h.db.Model(&dbRepeater).Select("TS1DynamicTalkgroupID").Updates(map[string]interface{}{"TS1DynamicTalkgroupID": nil})
			err := h.db.Model(&dbRepeater).Association("TS1DynamicTalkgroup").Delete(&dbRepeater.TS1DynamicTalkgroup)
			if err != nil {
				slog.Error("Error deleting TS1DynamicTalkgroup", "error", err)
			}
			h.unsubscribeTalkgroup(dbRepeater.ID, oldTGID, dmrconst.TimeslotOne)
		}
	}

	if err := h.db.Save(&dbRepeater).Error; err != nil {
		slog.Error("Error saving repeater", "error", err)
	}
}

// switchDynamicTalkgroup updates the dynamic talkgroup assignment for a repeater
// when it transmits a group call on a talkgroup it isn't currently linked to.
func (h *Hub) switchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Hub.switchDynamicTalkgroup")
	defer span.End()

	repeaterExists, err := models.RepeaterIDExists(h.db, packet.Repeater)
	if err != nil {
		slog.Error("Error checking if repeater exists", "repeaterID", packet.Repeater, "error", err)
		return
	}
	if !repeaterExists {
		return
	}

	talkgroupExists, err := models.TalkgroupIDExists(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error checking if talkgroup exists", "talkgroupID", packet.Dst, "error", err)
		return
	}
	if !talkgroupExists {
		return
	}

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

	if packet.Slot {
		if repeater.TS2DynamicTalkgroupID == nil || *repeater.TS2DynamicTalkgroupID != packet.Dst {
			slog.Info("Dynamically Linking timeslot 2", "repeaterID", packet.Repeater, "talkgroupID", packet.Dst)
			repeater.TS2DynamicTalkgroup = talkgroup
			repeater.TS2DynamicTalkgroupID = &packet.Dst
			h.subscribeTalkgroup(ctx, repeater.ID, packet.Dst)
			if err := h.db.Save(&repeater).Error; err != nil {
				slog.Error("Error saving repeater", "error", err)
			}
		}
	} else {
		if repeater.TS1DynamicTalkgroupID == nil || *repeater.TS1DynamicTalkgroupID != packet.Dst {
			slog.Info("Dynamically Linking timeslot 1", "repeaterID", packet.Repeater, "talkgroupID", packet.Dst)
			repeater.TS1DynamicTalkgroup = talkgroup
			repeater.TS1DynamicTalkgroupID = &packet.Dst
			h.subscribeTalkgroup(ctx, repeater.ID, packet.Dst)
			if err := h.db.Save(&repeater).Error; err != nil {
				slog.Error("Error saving repeater", "error", err)
			}
		}
	}
}
