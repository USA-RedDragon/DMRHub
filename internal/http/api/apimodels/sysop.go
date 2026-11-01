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

import "time"

// OnAirCall is a voice call that a repeater carries now. Direction is
// "from" for a call from a radio on the repeater and "to" for a call that
// the network sends to it. Slot is the timeslot of the call on the
// repeater, 1 or 2.
type OnAirCall struct {
	CallID      uint      `json:"call_id"`
	Slot        int       `json:"slot"`
	Direction   string    `json:"direction"`
	SourceID    uint      `json:"source_id"`
	Callsign    string    `json:"callsign"`
	Destination uint      `json:"destination_id"`
	GroupCall   bool      `json:"group_call"`
	StartTime   time.Time `json:"start_time"`
}

// DropCallPost names the call to drop on a repeater.
type DropCallPost struct {
	CallID uint `json:"call_id" binding:"required"`
}
