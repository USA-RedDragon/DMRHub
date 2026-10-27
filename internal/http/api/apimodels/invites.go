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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// Invite is an invite as an admin sees it, without its code.
type Invite struct {
	ID                uint       `json:"id"`
	CreatedBy         uint       `json:"created_by"`
	CreatedByCallsign string     `json:"created_by_callsign"`
	CreatedAt         time.Time  `json:"created_at"`
	ExpiresAt         time.Time  `json:"expires_at"`
	UsedBy            *uint      `json:"used_by"`
	UsedByCallsign    string     `json:"used_by_callsign,omitempty"`
	UsedAt            *time.Time `json:"used_at"`
}

// NewInvite returns the API form of inv, with the callsigns of the users
// by ID.
func NewInvite(inv models.Invite, callsigns map[uint]string) Invite {
	out := Invite{
		ID:                inv.ID,
		CreatedBy:         inv.CreatedBy,
		CreatedByCallsign: callsigns[inv.CreatedBy],
		CreatedAt:         inv.CreatedAt,
		ExpiresAt:         inv.ExpiresAt,
		UsedBy:            inv.UsedBy,
		UsedAt:            inv.UsedAt,
	}
	if inv.UsedBy != nil {
		out.UsedByCallsign = callsigns[*inv.UsedBy]
	}
	return out
}

// InviteList is the invites, newest first.
type InviteList struct {
	Invites []Invite `json:"invites"`
}

// NewInviteCode is a new invite with its code and the link that carries
// it, which DMRHub shows only once.
type NewInviteCode struct {
	Invite
	Code string `json:"code"`
	Link string `json:"link"`
}
