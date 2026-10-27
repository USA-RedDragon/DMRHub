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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"gorm.io/gorm"
)

// MessageAccessRadioID is a radio ID whose messages an admin saw, with the
// user who has it now, if any.
type MessageAccessRadioID struct {
	RadioID  uint   `json:"radio_id"`
	UserID   *uint  `json:"user_id"`
	Callsign string `json:"callsign,omitempty"`
}

// MessageAccess is one time that an admin saw stored messages.
type MessageAccess struct {
	ID            uint                     `json:"id"`
	At            time.Time                `json:"at"`
	AdminID       uint                     `json:"admin_id"`
	AdminCallsign string                   `json:"admin_callsign"`
	Kind          models.MessageAccessKind `json:"kind"`
	MessageID     *uint                    `json:"message_id"`
	Filter        string                   `json:"filter"`
	Results       int                      `json:"results"`
	RadioIDs      []MessageAccessRadioID   `json:"radio_ids"`
}

// MessageAccessList is a page of the message access log.
type MessageAccessList struct {
	Total    int64           `json:"total"`
	Accesses []MessageAccess `json:"accesses"`
}

// NewMessageAccessList returns the API form of rows, whose radio IDs ids
// holds by access ID, with the callsigns of the admins and of the users
// who have the radio IDs.
func NewMessageAccessList(db *gorm.DB, rows []models.MessageAccess, ids map[uint][]uint, total int64) (MessageAccessList, error) {
	out := MessageAccessList{Total: total, Accesses: make([]MessageAccess, 0, len(rows))}
	userIDs := map[uint]bool{}
	radioIDs := map[uint]bool{}
	for _, r := range rows {
		userIDs[r.AdminID] = true
		for _, id := range ids[r.ID] {
			radioIDs[id] = true
			userIDs[id] = true
		}
	}
	owner := map[uint]uint{}
	if len(radioIDs) > 0 {
		var extra []models.UserRadioID
		if err := db.Select("user_id", "radio_id").Where("radio_id IN ?", keys(radioIDs)).Find(&extra).Error; err != nil {
			return out, fmt.Errorf("could not find the users of the radio IDs: %w", err)
		}
		for _, e := range extra {
			owner[e.RadioID] = e.UserID
			userIDs[e.UserID] = true
		}
	}
	callsigns := map[uint]string{}
	if len(userIDs) > 0 {
		var users []models.User
		if err := db.Unscoped().Select("id", "callsign").Where("id IN ?", keys(userIDs)).Find(&users).Error; err != nil {
			return out, fmt.Errorf("could not find the users of the message accesses: %w", err)
		}
		for _, u := range users {
			callsigns[u.ID] = u.Callsign
		}
	}
	for _, r := range rows {
		a := MessageAccess{
			ID: r.ID, At: r.At, AdminID: r.AdminID, AdminCallsign: callsigns[r.AdminID], Kind: r.Kind,
			MessageID: r.MessageID, Filter: r.Filter, Results: r.Results, RadioIDs: []MessageAccessRadioID{},
		}
		for _, id := range ids[r.ID] {
			e := MessageAccessRadioID{RadioID: id}
			if _, ok := callsigns[id]; ok {
				uid := id
				e.UserID = &uid
			} else if uid, ok := owner[id]; ok {
				e.UserID = &uid
			}
			if e.UserID != nil {
				e.Callsign = callsigns[*e.UserID]
			}
			a.RadioIDs = append(a.RadioIDs, e)
		}
		out.Accesses = append(out.Accesses, a)
	}
	return out, nil
}

func keys(m map[uint]bool) []uint {
	out := make([]uint, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
