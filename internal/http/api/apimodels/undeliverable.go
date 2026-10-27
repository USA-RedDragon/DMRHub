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

package apimodels

import (
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"gorm.io/gorm"
)

// WSCallUndeliverable is a repeater that a call was not sent to, and why.
type WSCallUndeliverable struct {
	// Sentence is the sentence that the hub stored, such as "IPSC
	// repeater 300001 was busy on that slot.".
	Sentence string `json:"sentence"`
	// Target is the target server, such as "ipsc", or "".
	Target string `json:"target"`
	// RepeaterID is the repeater, or 0 for one that the sentence does not
	// name.
	RepeaterID uint `json:"repeater_id"`
	// Callsign is the callsign of the repeater, or "".
	Callsign string `json:"callsign"`
	// Reason is the reason code, such as "slot_busy", or "" for one that
	// the hub does not word.
	Reason string `json:"reason"`
}

// The scopes of the undeliverable list of a row. A row with no list has
// the scope "".
const (
	// UndeliverableScopeAll is the list of every repeater, which the
	// caller and an admin see.
	UndeliverableScopeAll = "all"
	// UndeliverableScopeOwn is the list of the viewer's own repeaters.
	UndeliverableScopeOwn = "own"
)

func undeliverableEntries(sentences []string) []WSCallUndeliverable {
	if len(sentences) == 0 {
		return nil
	}
	out := make([]WSCallUndeliverable, 0, len(sentences))
	for _, s := range sentences {
		e, _ := undeliverable.Parse(s)
		out = append(out, WSCallUndeliverable{Sentence: s, Target: e.Target, RepeaterID: e.Repeater, Reason: e.Reason})
	}
	return out
}

type undeliverableKey struct {
	peer bool
	id   uint
}

type undeliverableOwner struct {
	ID       uint
	Callsign string
	OwnerID  uint
}

func keyOf(e WSCallUndeliverable) undeliverableKey {
	return undeliverableKey{peer: e.Target == "openbridge", id: e.RepeaterID}
}

func undeliverableOwners(db *gorm.DB, rows []WSCallResponse) (map[undeliverableKey]undeliverableOwner, error) {
	var repeaters, peers []uint
	for i := range rows {
		for _, e := range rows[i].Undeliverable {
			if e.RepeaterID == 0 {
				continue
			}
			if keyOf(e).peer {
				peers = append(peers, e.RepeaterID)
			} else {
				repeaters = append(repeaters, e.RepeaterID)
			}
		}
	}
	out := map[undeliverableKey]undeliverableOwner{}
	for _, q := range []struct {
		model any
		ids   []uint
		peer  bool
		cols  []string
	}{
		{&models.Repeater{}, repeaters, false, []string{"id", "callsign", "owner_id"}},
		{&models.Peer{}, peers, true, []string{"id", "owner_id"}},
	} {
		if len(q.ids) == 0 {
			continue
		}
		var found []undeliverableOwner
		if err := db.Model(q.model).Select(q.cols).Where("id IN ?", q.ids).Scan(&found).Error; err != nil {
			return nil, fmt.Errorf("failed to find the repeaters that a call was not sent to: %w", err)
		}
		for _, f := range found {
			out[undeliverableKey{peer: q.peer, id: f.ID}] = f
		}
	}
	return out, nil
}

// showUndeliverable leaves in each row the undeliverable entries that
// viewer may see, with the callsign of each repeater. The caller and an
// admin see every entry, the owner of a repeater the entries of their own
// repeaters, and anyone else none.
func showUndeliverable(db *gorm.DB, viewer *models.User, rows []WSCallResponse) error {
	if viewer == nil {
		for i := range rows {
			rows[i].Undeliverable, rows[i].UndeliverableScope = nil, ""
		}
		return nil
	}
	owners, err := undeliverableOwners(db, rows)
	if err != nil {
		return err
	}
	admin := models.IsAdmin(viewer)
	for i := range rows {
		r := &rows[i]
		all := admin || isCaller(viewer, r)
		kept := r.Undeliverable[:0]
		for _, e := range r.Undeliverable {
			o, known := owners[keyOf(e)]
			if !all && (!known || o.OwnerID != viewer.ID) {
				continue
			}
			e.Callsign = o.Callsign
			kept = append(kept, e)
		}
		r.Undeliverable, r.UndeliverableScope = nil, ""
		if len(kept) > 0 {
			r.Undeliverable, r.UndeliverableScope = kept, UndeliverableScopeOwn
			if all {
				r.UndeliverableScope = UndeliverableScopeAll
			}
		}
	}
	return nil
}

func isCaller(viewer *models.User, r *WSCallResponse) bool {
	return viewer != nil && viewer.ID == r.SourceID
}
