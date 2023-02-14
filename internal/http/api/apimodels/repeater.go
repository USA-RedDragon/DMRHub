// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

type RepeaterPost struct {
	RadioID uint `json:"id" binding:"required"`
}

type RepeaterTalkgroupsPost struct {
	TS1StaticTalkgroups []models.Talkgroup `json:"ts1_static_talkgroups"`
	TS2StaticTalkgroups []models.Talkgroup `json:"ts2_static_talkgroups"`
	TS1DynamicTalkgroup models.Talkgroup   `json:"ts1_dynamic_talkgroup"`
	TS2DynamicTalkgroup models.Talkgroup   `json:"ts2_dynamic_talkgroup"`
}
