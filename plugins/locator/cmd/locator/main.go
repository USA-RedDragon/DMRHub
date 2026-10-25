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

// Command locator is a DMRHub plugin that serves ARS and location to
// radios. Add it on the Plugins page of the hub with the IDs radios send
// their positions to, such as 5050 to 5059, then run it with the address,
// token and fingerprint that the page shows:
//
//	DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... dmrhub-locator -address hub.example.org:3006 -fingerprint 4F:2A:...
//
// The address and fingerprint can also come from DMRHUB_PLUGIN_ADDRESS and
// DMRHUB_PLUGIN_FINGERPRINT.
package main

import (
	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
)

func main() {
	plugin.MainWith(locator.Hello(), locator.Serve)
}
