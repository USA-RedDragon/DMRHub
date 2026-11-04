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

package mailbox

import pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"

// EmailSwitch is the key of the switch with which a user asks for an email
// when a message waits.
const EmailSwitch = "email"

// Hello says who the mailbox is. It asks for no IDs, so it starts off
// until an admin gives it one.
func Hello() *pluginv1.Hello {
	return &pluginv1.Hello{
		Name:   "Mailbox",
		Author: "DMRHub",
		Description: "Keeps text messages for callsigns. Text the mailbox ID with a callsign and a message to leave one, " +
			"and INBOX, GET 1, DELETE 1 or DELETE ALL to read yours.",
		Switches: []*pluginv1.Switch{{
			Key:         EmailSwitch,
			Title:       "Mailbox email notice",
			Description: "Email me when a message waits for my callsign in the mailbox.",
			Details: []*pluginv1.SwitchDetail{
				{Label: "To read your messages", Value: "Text `INBOX` to {id}"},
			},
		}},
	}
}
