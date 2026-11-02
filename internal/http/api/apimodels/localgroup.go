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

import "github.com/USA-RedDragon/DMRHub/internal/db/models"

// LocalGroupPost makes a local group or changes it.
type LocalGroupPost struct {
	Name      string `json:"name"`
	Talkgroup uint   `json:"talkgroup"`
}

// LocalGroupRepeaterPost adds a repeater to a local group on a timeslot,
// 1 or 2; 0 is 2.
type LocalGroupRepeaterPost struct {
	RepeaterID uint  `json:"repeater_id" binding:"required"`
	Slot       uint8 `json:"slot"`
}

// LocalGroupRepeaterPatch sets the timeslot, 1 or 2, of a repeater in a
// local group.
type LocalGroupRepeaterPatch struct {
	Slot uint8 `json:"slot"`
}

// LocalGroupRepeater is a repeater in a local group. State is member, or
// asked while its owner has not said yes. Mine marks a repeater that the
// viewer owns or is a co-sysop of.
type LocalGroupRepeater struct {
	ID       uint           `json:"id"`
	Callsign string         `json:"callsign"`
	Owner    models.UserRef `json:"owner"`
	Slot     uint8          `json:"slot"`
	State    string         `json:"state"`
	Mine     bool           `json:"mine"`
}

// LocalGroup is a local group as a viewer sees it. Members counts the
// repeaters in it. Mine marks a group of the viewer, CanManage one the
// viewer can change, and Involved one that a repeater that the viewer owns
// or is a co-sysop of is in or asked to join.
type LocalGroup struct {
	ID        uint                 `json:"id"`
	Name      string               `json:"name"`
	Talkgroup uint                 `json:"talkgroup"`
	Owner     models.UserRef       `json:"owner"`
	Members   int                  `json:"members"`
	Mine      bool                 `json:"mine"`
	CanManage bool                 `json:"can_manage"`
	Involved  bool                 `json:"involved"`
	Repeaters []LocalGroupRepeater `json:"repeaters"`
}

// LocalGroupList is the local groups that a viewer sees, and the local
// talkgroup numbers that groups can use.
type LocalGroupList struct {
	Groups     []LocalGroup `json:"groups"`
	Talkgroups []uint       `json:"talkgroups"`
}

// RepeaterLocalGroup is a local group that a repeater is in or asked to
// join, as the page of the repeater shows it.
type RepeaterLocalGroup struct {
	ID        uint           `json:"id"`
	Name      string         `json:"name"`
	Talkgroup uint           `json:"talkgroup"`
	Slot      uint8          `json:"slot"`
	State     string         `json:"state"`
	Owner     models.UserRef `json:"owner"`
	AskedBy   models.UserRef `json:"asked_by"`
	Repeaters int            `json:"repeaters"`
}
