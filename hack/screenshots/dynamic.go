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

package main

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"
)

// dynamicTimeout is the dynamic talkgroup timeout of DMRHub, its default,
// which the demo network keeps: after it with no group call from a
// repeater timeslot, DMRHub unlinks the dynamic talkgroup of that slot.
const dynamicTimeout = 15 * time.Minute

type repeaterSlot struct {
	repeater uint32
	slot2    bool
}

// heardGroup notes that the repeater sent a group call on the timeslot at
// the time of the demo clock, which links a dynamic talkgroup there.
func (r *runner) heardGroup(repeater uint32, slot2 bool) {
	r.groups[repeaterSlot{repeater, slot2}] = r.cursor
}

// waitDynamicOfHub is waitDynamic on the database of DMRHub.
func (r *runner) waitDynamicOfHub(ctx context.Context) error {
	if len(r.groups) == 0 {
		return nil
	}
	db, closeDB, err := demoDB(r)
	if err != nil {
		return err
	}
	defer closeDB()
	return r.waitDynamic(ctx, db)
}

// waitDynamic waits until DMRHub unlinked each dynamic talkgroup whose
// last group call from the harness is dynamicTimeout or more before the
// demo clock. DMRHub unlinks them on a sweep that runs on a real-time
// timer, so after a jump of the clock a page can load before it ran. A
// link that the harness did not make is left alone.
func (r *runner) waitDynamic(ctx context.Context, db *gorm.DB) error {
	report := time.Now().Add(stuckReport)
	for {
		var rows []struct {
			ID  uint32
			TS1 *uint
			TS2 *uint
		}
		if err := db.Raw("SELECT id, ts1_dynamic_talkgroup_id AS ts1, ts2_dynamic_talkgroup_id AS ts2 FROM repeaters WHERE deleted_at IS NULL AND (ts1_dynamic_talkgroup_id IS NOT NULL OR ts2_dynamic_talkgroup_id IS NOT NULL)").Scan(&rows).Error; err != nil {
			return fmt.Errorf("could not read the dynamic talkgroups: %w", err)
		}
		var due []repeaterSlot
		for _, row := range rows {
			for _, s := range []struct {
				tg    *uint
				slot2 bool
			}{{row.TS1, false}, {row.TS2, true}} {
				key := repeaterSlot{row.ID, s.slot2}
				if at, ok := r.groups[key]; s.tg != nil && ok && r.cursor.Sub(at) >= dynamicTimeout {
					due = append(due, key)
				}
			}
		}
		if len(due) == 0 {
			return nil
		}
		if time.Now().After(report) {
			slog.Warn("DMRHub has not unlinked expired dynamic talkgroups yet", "slots", due)
			report = time.Now().Add(stuckReport)
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("waiting for the end of the dynamic talkgroups: %w", ctx.Err())
		case <-time.After(requestStep):
		}
	}
}
