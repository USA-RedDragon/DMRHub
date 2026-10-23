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
	"log/slog"
	"strings"

	"gorm.io/gorm"
)

// SubKindSMS is the SubKind of a data transmission that carried a text
// message.
const SubKindSMS = "sms"

// SMSWindow decides which transmissions a user may see as a message in
// last heard. Outside the window, a user sees a transmission that carried a
// message as plain data, so that last heard tells nothing about messages
// that the Messages page does not show.
type SMSWindow interface {
	// VisibleSMS returns a query that selects the id column of the calls
	// whose message is inside the visible window of viewer, or nil when no
	// message is. A nil viewer is a visitor who is not logged in.
	VisibleSMS(db *gorm.DB, viewer *User) *gorm.DB
	// Visible returns which of the calls ids carried a message inside the
	// visible window of viewer, who is not an admin.
	Visible(db *gorm.DB, viewer *User, ids []uint) (map[uint]bool, error)
}

// MaskedSMSWindow shows no message to any user.
type MaskedSMSWindow struct{}

// VisibleSMS returns nil: no message is visible.
func (MaskedSMSWindow) VisibleSMS(*gorm.DB, *User) *gorm.DB {
	return nil
}

// Visible returns an empty map: no message is visible.
func (MaskedSMSWindow) Visible(*gorm.DB, *User, []uint) (map[uint]bool, error) {
	return map[uint]bool{}, nil
}

// MessageSMSWindow shows a user the transmissions of the messages in their
// capped inbox and outbox windows, as VisibleMessageIDs finds them.
type MessageSMSWindow struct{}

// VisibleSMS returns a query of the calls that carried a message in the
// boxes of viewer, or nil when the boxes are empty.
func (MessageSMSWindow) VisibleSMS(db *gorm.DB, viewer *User) *gorm.DB {
	ids, err := visibleMessages(db, nonAdmin(viewer))
	if err != nil {
		slog.Error("Could not find the visible messages of a user", "error", err)
		return nil
	}
	if len(ids) == 0 {
		return nil
	}
	return db.Model(&Message{}).Select("transmission_id").Where("transmission_id IS NOT NULL AND id IN ?", ids)
}

// Visible returns VisibleTransmissions of viewer.
func (MessageSMSWindow) Visible(db *gorm.DB, viewer *User, ids []uint) (map[uint]bool, error) {
	return VisibleTransmissions(db, nonAdmin(viewer), ids)
}

func nonAdmin(viewer *User) *User {
	if viewer == nil || !viewer.Admin {
		return viewer
	}
	u := *viewer
	u.Admin = false
	return &u
}

// CurrentSMSWindow returns the SMS window of the hub: the message boxes of
// each user.
func CurrentSMSWindow() SMSWindow {
	return MessageSMSWindow{}
}

// CallIDsIn returns which of the calls ids the query visible selects. A nil
// query selects none.
func CallIDsIn(db *gorm.DB, ids []uint, visible *gorm.DB) (map[uint]bool, error) {
	out := map[uint]bool{}
	if visible == nil || len(ids) == 0 {
		return out, nil
	}
	var got []uint
	if err := db.Session(&gorm.Session{NewDB: true}).Model(&Call{}).Where("id IN ?", ids).Where("id IN (?)", visible).Pluck("id", &got).Error; err != nil {
		return nil, fmt.Errorf("failed to find visible calls: %w", err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// TransmissionsWithMessages returns which of the calls ids carried a
// stored message.
func TransmissionsWithMessages(db *gorm.DB, ids []uint) (map[uint]bool, error) {
	out := map[uint]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	var got []uint
	if err := db.Session(&gorm.Session{NewDB: true}).Model(&Message{}).Where("transmission_id IN ?", ids).Distinct().Pluck("transmission_id", &got).Error; err != nil {
		return nil, fmt.Errorf("failed to find the transmissions of messages: %w", err)
	}
	for _, id := range got {
		out[id] = true
	}
	return out, nil
}

// IsAdmin reports whether viewer is an approved admin who is not suspended.
// A nil viewer is not.
func IsAdmin(viewer *User) bool {
	return viewer != nil && viewer.Admin && viewer.Approved && !viewer.Suspended
}

// The types of a last heard filter. TypeSMS is a data transmission that
// carried an SMS, and TypeControl is UDT, MBC and, with Idle, idle and USBD.
const (
	TypeVoice   = "voice"
	TypeData    = "data"
	TypeSMS     = "sms"
	TypeGPS     = "gps"
	TypeAlias   = "alias"
	TypeCSBK    = "csbk"
	TypeControl = "control"
	TypeUnknown = "unknown"
)

// DefaultCallTypes returns the types that last heard shows when the request
// names none: every type but unknown.
func DefaultCallTypes() []string {
	return []string{TypeVoice, TypeData, TypeSMS, TypeGPS, TypeAlias, TypeCSBK, TypeControl}
}

// CallFilter selects the transmissions that a last heard list shows.
type CallFilter struct {
	// Types are the types to show. Nil means DefaultCallTypes.
	Types []string
	// Idle shows idle and USBD transmissions with TypeControl.
	Idle bool
}

// ParseCallFilter returns the filter of a comma-separated list of types and
// the idle flag. It returns an error for an unknown type.
func ParseCallFilter(types string, idle bool) (CallFilter, error) {
	f := CallFilter{Idle: idle}
	if types == "" {
		return f, nil
	}
	f.Types = []string{}
	for _, t := range strings.Split(types, ",") {
		t = strings.TrimSpace(t)
		switch t {
		case TypeVoice, TypeData, TypeSMS, TypeGPS, TypeAlias, TypeCSBK, TypeControl, TypeUnknown:
			f.Types = append(f.Types, t)
		case "":
		default:
			return CallFilter{}, fmt.Errorf("unknown transmission type %q", t)
		}
	}
	return f, nil
}

// Scope returns a scope that keeps the calls of the filter as viewer sees
// them: for a viewer who is not an admin, an SMS outside the SMS window of
// window counts as data. A visitor who is not logged in sees no SMS.
func (f CallFilter) Scope(db *gorm.DB, viewer *User, window SMSWindow) func(*gorm.DB) *gorm.DB {
	types := f.Types
	if types == nil {
		types = DefaultCallTypes()
	}
	var visible *gorm.DB
	admin := IsAdmin(viewer)
	if !admin && viewer != nil && window != nil {
		visible = window.VisibleSMS(db.Session(&gorm.Session{NewDB: true}), viewer)
	}
	return func(tx *gorm.DB) *gorm.DB {
		cond := db.Session(&gorm.Session{NewDB: true}).Where("1 = 0")
		kinds := []CallKind{}
		for _, t := range types {
			switch t {
			case TypeVoice:
				kinds = append(kinds, CallKindVoice)
			case TypeGPS:
				kinds = append(kinds, CallKindGPS)
			case TypeAlias:
				kinds = append(kinds, CallKindAlias)
			case TypeCSBK:
				kinds = append(kinds, CallKindCSBK)
			case TypeUnknown:
				kinds = append(kinds, CallKindUnknown)
			case TypeControl:
				kinds = append(kinds, CallKindUDT, CallKindMBC)
				if f.Idle {
					kinds = append(kinds, CallKindIdle, CallKindUSBD)
				}
			case TypeData:
				switch {
				case admin:
					cond = cond.Or("kind = ? AND COALESCE(sub_kind, '') <> ?", CallKindData, SubKindSMS)
				case visible == nil:
					cond = cond.Or("kind = ?", CallKindData)
				default:
					cond = cond.Or("kind = ? AND (COALESCE(sub_kind, '') <> ? OR id NOT IN (?))", CallKindData, SubKindSMS, visible)
				}
			case TypeSMS:
				switch {
				case admin:
					cond = cond.Or("kind = ? AND sub_kind = ?", CallKindData, SubKindSMS)
				case visible != nil:
					cond = cond.Or("kind = ? AND sub_kind = ? AND id IN (?)", CallKindData, SubKindSMS, visible)
				}
			}
		}
		if len(kinds) > 0 {
			cond = cond.Or("kind IN ?", kinds)
		}
		return tx.Where(cond)
	}
}

// VisibleSMSIDs returns the IDs among ids of the transmissions that viewer
// may see as messages. An admin sees every one, and a visitor who is not
// logged in sees none.
func VisibleSMSIDs(db *gorm.DB, viewer *User, window SMSWindow, ids []uint) (map[uint]bool, error) {
	out := map[uint]bool{}
	if len(ids) == 0 {
		return out, nil
	}
	if IsAdmin(viewer) {
		for _, id := range ids {
			out[id] = true
		}
		return out, nil
	}
	if viewer == nil || window == nil {
		return out, nil
	}
	visible, err := window.Visible(db.Session(&gorm.Session{NewDB: true}), viewer, ids)
	if err != nil {
		return nil, fmt.Errorf("failed to find visible messages: %w", err)
	}
	return visible, nil
}
