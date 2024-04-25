// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

type WSCallResponseUser struct {
	ID       uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseRepeater struct {
	RadioID  uint   `json:"id"`
	Callsign string `json:"callsign"`
}

type WSCallResponseTalkgroup struct {
	ID          uint   `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type WSCallResponse struct {
	ID            uint                    `json:"id"`
	User          WSCallResponseUser      `json:"user"`
	StartTime     time.Time               `json:"start_time"`
	Duration      time.Duration           `json:"duration"`
	Active        bool                    `json:"active"`
	TimeSlot      bool                    `json:"time_slot"`
	GroupCall     bool                    `json:"group_call"`
	IsToTalkgroup bool                    `json:"is_to_talkgroup"`
	ToTalkgroup   WSCallResponseTalkgroup `json:"to_talkgroup"`
	IsToUser      bool                    `json:"is_to_user"`
	ToUser        WSCallResponseUser      `json:"to_user"`
	IsToRepeater  bool                    `json:"is_to_repeater"`
	ToRepeater    WSCallResponseRepeater  `json:"to_repeater"`
	Loss          float32                 `json:"loss"`
	Jitter        float32                 `json:"jitter"`
	BER           float32                 `json:"ber"`
	RSSI          float32                 `json:"rssi"`
}
