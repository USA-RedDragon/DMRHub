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

// Command guest is the plugins that come with DMRHub, as one WebAssembly
// module for wasip1, so that DMRHub compiles their code once for all of
// them. Its argument after the program name picks the plugin. The gen
// command builds it.
package main

import (
	"fmt"
	"os"

	"github.com/USA-RedDragon/DMRHub/plugins/locator"
	"github.com/USA-RedDragon/DMRHub/plugins/parrot"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
)

func main() {
	key := ""
	if len(os.Args) > 1 {
		key = os.Args[1]
	}
	switch key {
	case "parrot":
		plugin.MainWith(parrot.Hello(), parrot.Serve)
	case "locator":
		plugin.MainWith(locator.Hello(), locator.Serve)
	default:
		fmt.Fprintln(os.Stderr, "no such built-in plugin:", key)
		os.Exit(2)
	}
}
