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

package models

import (
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"
)

// MessageBox is the inbox or the outbox of a user.
type MessageBox string

// The MessageBox values. The inbox holds private messages to the radio IDs
// of a user; the outbox holds messages from them, group messages included.
const (
	MessageInbox  MessageBox = "inbox"
	MessageOutbox MessageBox = "outbox"
)

// MessageBoxCap is the number of messages a user sees in each box: the
// newest ones.
const MessageBoxCap = 250

type ownedID struct {
	id   uint
	from time.Time
}

func ownedIDs(db *gorm.DB, user *User) ([]ownedID, error) {
	if user == nil || !user.SMSStorageEnabled || user.SMSStorageSince == nil {
		return nil, nil
	}
	since := *user.SMSStorageSince
	out := []ownedID{{id: user.ID, from: since}}
	added, err := ListUserRadioIDs(db, user.ID)
	if err != nil {
		return nil, err
	}
	for _, r := range added {
		from := since
		if r.VerifiedAt.After(from) {
			from = r.VerifiedAt
		}
		out = append(out, ownedID{id: r.RadioID, from: from})
	}
	return out, nil
}

// VisibleMessageIDs returns the IDs of the messages of box that user sees,
// newest first: with SMS Storage on, the messages to (inbox) or from
// (outbox) a radio ID that the user owns, received after the user turned
// SMS Storage on and after the ID became theirs, at most MessageBoxCap of
// them. Every route that shows messages to a user filters through it.
func VisibleMessageIDs(db *gorm.DB, user *User, box MessageBox) ([]uint, error) {
	owned, err := ownedIDs(db, user)
	if err != nil || len(owned) == 0 {
		return nil, err
	}
	column := "destination_id"
	if box == MessageOutbox {
		column = "source_id"
	}
	parts := make([]string, 0, len(owned))
	args := make([]any, 0, 2*len(owned))
	for _, o := range owned {
		parts = append(parts, "("+column+" = ? AND received_at >= ?)")
		args = append(args, o.id, o.from.UTC())
	}
	q := db.Model(&Message{}).Where(strings.Join(parts, " OR "), args...)
	if box == MessageInbox {
		q = q.Where("is_group = ?", false)
	}
	var ids []uint
	if err := q.Order("received_at DESC").Order("id DESC").Limit(MessageBoxCap).Pluck("id", &ids).Error; err != nil {
		return nil, fmt.Errorf("could not list visible messages: %w", err)
	}
	return ids, nil
}

// MessageBoxes returns the boxes of user that hold the message id, none
// when the user does not see it.
func MessageBoxes(db *gorm.DB, user *User, id uint) ([]MessageBox, error) {
	var boxes []MessageBox
	for _, box := range []MessageBox{MessageInbox, MessageOutbox} {
		ids, err := VisibleMessageIDs(db, user, box)
		if err != nil {
			return nil, err
		}
		for _, v := range ids {
			if v == id {
				boxes = append(boxes, box)
				break
			}
		}
	}
	return boxes, nil
}

// VisibleTransmissions returns which of the transmissions callIDs carried a
// message that user sees in a box. An admin sees every transmission. Last
// heard uses it to hide the kind and size of the data rows of messages that
// a user does not see.
func VisibleTransmissions(db *gorm.DB, user *User, callIDs []uint) (map[uint]bool, error) {
	out := make(map[uint]bool, len(callIDs))
	if user == nil || len(callIDs) == 0 {
		return out, nil
	}
	if user.Admin {
		for _, id := range callIDs {
			out[id] = true
		}
		return out, nil
	}
	var visible []uint
	for _, box := range []MessageBox{MessageInbox, MessageOutbox} {
		ids, err := VisibleMessageIDs(db, user, box)
		if err != nil {
			return nil, err
		}
		visible = append(visible, ids...)
	}
	if len(visible) == 0 {
		return out, nil
	}
	var rows []uint
	if err := db.Model(&Message{}).Where("id IN ? AND transmission_id IN ?", visible, callIDs).Distinct().Pluck("transmission_id", &rows).Error; err != nil {
		return nil, fmt.Errorf("could not find visible transmissions: %w", err)
	}
	for _, id := range rows {
		out[id] = true
	}
	return out, nil
}
