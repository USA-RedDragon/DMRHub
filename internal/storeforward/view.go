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

package storeforward

import (
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"gorm.io/gorm"
)

func waiting() []models.HeldState {
	return []models.HeldState{models.HeldStateHeld, models.HeldStateSending}
}

type formatCache struct {
	db    *gorm.DB
	known map[uint]bool
}

func (c *formatCache) of(radio uint) bool {
	if k, ok := c.known[radio]; ok {
		return k
	}
	_, err := RadioFormat(c.db, radio)
	c.known[radio] = err == nil
	return err == nil
}

// HeldFor returns the texts that store and forward keeps for the radios of
// the user userID, oldest first.
func HeldFor(db *gorm.DB, userID uint) (apimodels.HeldTextList, error) {
	var rows []models.HeldMessage
	if err := db.Preload("Message").Where("user_id = ? AND state IN ?", userID, waiting()).
		Order("held_at").Order("id").Find(&rows).Error; err != nil {
		return apimodels.HeldTextList{}, fmt.Errorf("storeforward: listing the held texts of user %d: %w", userID, err)
	}
	formats := &formatCache{db: db, known: map[uint]bool{}}
	out := apimodels.HeldTextList{Held: make([]apimodels.HeldText, 0, len(rows))}
	for i := range rows {
		r := &rows[i]
		if r.Message == nil {
			continue
		}
		h := apimodels.HeldText{
			ID: r.ID, MessageID: r.MessageID, SourceID: r.Message.SourceID, RadioID: r.RadioID,
			HeldAt: r.HeldAt.UTC(), FormatKnown: formats.of(r.RadioID),
		}
		if r.Message.Text != nil {
			h.Text = *r.Message.Text
		}
		h.SourceCallsign = callsign(db, r.Message.SourceID)
		out.Held = append(out.Held, h)
	}
	return out, nil
}

func callsign(db *gorm.DB, id uint) string {
	var u models.User
	if err := db.Select("id", "callsign").Where("id = ?", id).Limit(1).Find(&u).Error; err == nil && u.Callsign != "" {
		return u.Callsign
	}
	if u, ok := userdb.Get(id); ok {
		return u.Callsign
	}
	return ""
}

// HeldOf returns what store and forward did with each of the messages
// msgs that it held, by message ID.
func HeldOf(db *gorm.DB, msgs []models.Message) (map[uint]*apimodels.Held, error) {
	out := map[uint]*apimodels.Held{}
	if len(msgs) == 0 {
		return out, nil
	}
	ids := make([]uint, 0, len(msgs))
	for i := range msgs {
		if !msgs[i].IsGroup {
			ids = append(ids, msgs[i].ID)
		}
	}
	if len(ids) == 0 {
		return out, nil
	}
	var rows []models.HeldMessage
	if err := db.Where("message_id IN ?", ids).Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("storeforward: finding the held messages: %w", err)
	}
	formats := &formatCache{db: db, known: map[uint]bool{}}
	for i := range rows {
		r := &rows[i]
		h := &apimodels.Held{State: string(r.State), Response: string(r.Response), FormatKnown: true}
		if h.Response == "" {
			h.Response = "none"
		}
		if r.DoneAt != nil {
			at := r.DoneAt.UTC()
			h.DoneAt = &at
		}
		if r.State == models.HeldStateHeld || r.State == models.HeldStateSending {
			h.FormatKnown = formats.of(r.RadioID)
		}
		out[r.MessageID] = h
	}
	return out, nil
}
