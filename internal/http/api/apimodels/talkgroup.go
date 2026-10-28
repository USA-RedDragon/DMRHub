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

type TalkgroupPost struct {
	ID          uint   `json:"id" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description" binding:"required"`
}

type TalkgroupPatch struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type TalkgroupAdminAction struct {
	UserIDs []uint `json:"user_ids"`
}

// The modes of TalkgroupAccess.
const (
	TalkgroupAccessOpen     = "open"
	TalkgroupAccessApproved = "approved"
)

// RepeaterRef is a repeater inside another model: its ID and callsign only.
type RepeaterRef struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

// TalkgroupAccess is who may link a talkgroup. Mode is open (every
// repeater) or approved (only Repeaters).
type TalkgroupAccess struct {
	Mode      string        `json:"mode"`
	Repeaters []RepeaterRef `json:"repeaters"`
}

// TalkgroupAccessPatch changes who may link a talkgroup. A field that is
// left out keeps its value.
type TalkgroupAccessPatch struct {
	Mode      *string `json:"mode"`
	Repeaters *[]uint `json:"repeaters"`
}
