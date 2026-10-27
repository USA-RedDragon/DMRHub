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

type RepeaterPost struct {
	RadioID         uint   `json:"id" binding:"required"`
	Type            string `json:"type"`
	SimplexRepeater bool   `json:"simplex_repeater"`
}

// RepeaterOwnerPost is the owner that an admin assigns to a repeater with
// no owner.
type RepeaterOwnerPost struct {
	Owner uint `json:"owner" binding:"required"`
}

type RepeaterTalkgroupsPost struct {
	TS1StaticTalkgroups []models.Talkgroup `json:"ts1_static_talkgroups"`
	TS2StaticTalkgroups []models.Talkgroup `json:"ts2_static_talkgroups"`
	TS1DynamicTalkgroup models.Talkgroup   `json:"ts1_dynamic_talkgroup"`
	TS2DynamicTalkgroup models.Talkgroup   `json:"ts2_dynamic_talkgroup"`
}

type RepeaterPatch struct {
	SimplexRepeater *bool `json:"simplex_repeater"`
	// Trusted lets the hub route private traffic to a radio from where this
	// repeater last heard it, whoever owns the radio. Only an admin may set
	// it.
	Trusted *bool `json:"trusted"`
	// TS1Codec and TS2Codec set the codec of each timeslot: "auto",
	// "codec2" or "ambe".
	TS1Codec *string `json:"ts1_codec"`
	TS2Codec *string `json:"ts2_codec"`
}

// RepeaterRotatePasswordResponse is the answer to a rotation of the password
// of a repeater: the new password, or the new auth key of an IPSC
// repeater, and when the old one stopped working.
type RepeaterRotatePasswordResponse struct {
	Password  string    `json:"password"`
	RotatedAt time.Time `json:"rotated_at"`
}
