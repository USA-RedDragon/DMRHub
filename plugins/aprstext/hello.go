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

package aprstext

import (
	"strconv"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// SwitchKey is the key of the switch that a user turns on to send and get
// APRS messages.
const SwitchKey = "aprs"

// PresetSSID is the SSID of the APRS address of a user who did not choose
// one.
const PresetSSID = "7"

// Hello says who the plugin is. It asks for no IDs, so it starts off until
// an admin gives it one, and declares the switch that users turn on in
// Settings.
func Hello() *pluginv1.Hello {
	ssids := make([]string, 0, 15)
	for i := 1; i <= 15; i++ {
		ssids = append(ssids, strconv.Itoa(i))
	}
	return &pluginv1.Hello{
		Name:        "APRS messages",
		Author:      "DMRHub",
		Description: "Sends the text messages that users send to its ID to APRS-IS as APRS messages, and the APRS messages to their APRS addresses to their radios.",
		Switches: []*pluginv1.Switch{{
			Key:         SwitchKey,
			Title:       "APRS messages",
			Description: "Send and get APRS text messages with your radio.",
			Notice:      "APRS messages go out on the internet, where anyone can read them.",
			Details: []*pluginv1.SwitchDetail{
				{Label: "Your APRS address", Value: "`{callsign}-{choice}`"},
				{Label: "To send", Value: "Send a private text message to `{id}`. Type the APRS address, a space, then your message, such as `N0CALL-9 See you at six`."},
				{Label: "To get", Value: "Messages to `{callsign}-{choice}` come to your radio from `{id}`, such as `N0CALL-9: See you at six`."},
			},
			Choice: &pluginv1.SwitchChoice{Label: "SSID", Options: ssids, Preset: PresetSSID},
		}},
	}
}
