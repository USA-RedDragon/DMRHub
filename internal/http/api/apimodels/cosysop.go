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

// CosysopPost invites a user to be a co-sysop of a repeater. User is the
// callsign or the username of the user.
type CosysopPost struct {
	User string `json:"user" binding:"required"`
}

// Cosysop is a co-sysop of a repeater, or a user with an invite to be one
// when Pending is true.
type Cosysop struct {
	ID         uint       `json:"id"`
	Callsign   string     `json:"callsign"`
	Username   string     `json:"username"`
	InvitedAt  time.Time  `json:"invited_at"`
	AcceptedAt *time.Time `json:"accepted_at"`
	ExpiresAt  *time.Time `json:"expires_at"`
	Pending    bool       `json:"pending"`
}

// Cosysops lists the owner and the co-sysops of a repeater. Role is what
// the user who asked is to the repeater: owner, cosysop or admin.
type Cosysops struct {
	Owner    *models.UserRef `json:"owner"`
	Role     string          `json:"role"`
	Cosysops []Cosysop       `json:"cosysops"`
}

// CosysopInvite is an invite of the user who asked to be a co-sysop of a
// repeater.
type CosysopInvite struct {
	RepeaterID       uint            `json:"repeater_id"`
	RepeaterCallsign string          `json:"repeater_callsign"`
	Owner            *models.UserRef `json:"owner"`
	InvitedAt        time.Time       `json:"invited_at"`
	ExpiresAt        time.Time       `json:"expires_at"`
}
