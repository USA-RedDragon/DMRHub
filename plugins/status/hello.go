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

package status

import pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"

// ID is the DMR ID that radios usually call for the status of their
// repeater or hotspot.
const ID = 5000

// Hello says who the status service is, and that it asks for 5000.
func Hello() *pluginv1.Hello {
	return &pluginv1.Hello{
		Name:        "Status",
		Author:      "DMRHub",
		Description: "Answers a private call to its ID with the talkgroups of the caller's timeslot as spoken prompts, and a text to its ID with the same as a text, in the format the radio sent.",
		Ids:         []*pluginv1.IDRange{{First: ID, Last: ID}},
	}
}
