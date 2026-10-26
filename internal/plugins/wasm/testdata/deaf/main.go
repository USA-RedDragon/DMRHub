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

// Command deaf is a test plugin that says hello and then never reads what
// the hub sends it.
package main

import (
	"os"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

func main() {
	hello := &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Hello{Hello: &pluginv1.Hello{Name: "Deaf", Version: "1.0.0"}}}
	if err := plugin.WriteFrame(os.Stdout, hello); err != nil {
		os.Exit(1)
	}
	for {
		time.Sleep(time.Hour)
	}
}
