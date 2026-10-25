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

package parrot

import pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"

// Hello says who the parrot is, and that it asks for 9990, the ID radios
// usually call the parrot at.
func Hello() *pluginv1.Hello {
	return &pluginv1.Hello{
		Name:        "Parrot",
		Author:      "DMRHub",
		Description: "Plays each voice call to its IDs back to the caller, three seconds after the call ends.",
		Ids:         []*pluginv1.IDRange{{First: 9990, Last: 9990}},
	}
}
