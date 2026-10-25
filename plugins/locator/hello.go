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

package locator

import pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"

// Hello says who the location server is, and that it asks for 5050 to
// 5059, the IDs that BrandMeister uses.
func Hello() *pluginv1.Hello {
	return &pluginv1.Hello{
		Name:        "Location server",
		Author:      "DMRHub",
		Description: "Answers the registrations and location reports of radios, and asks the radios whose owners turned tracking on for their positions.",
		Ids:         []*pluginv1.IDRange{{First: 5050, Last: 5059}},
	}
}
