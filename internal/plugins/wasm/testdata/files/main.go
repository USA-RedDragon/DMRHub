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

// Command files is a test plugin that tries to read and write files and to
// read the environment, and says what it found in its hello.
package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

func main() {
	var found []string
	for _, p := range []string{"/etc/passwd", "/", ".", "/proc/self/environ"} {
		if _, err := os.ReadFile(p); err == nil {
			found = append(found, "read "+p)
		}
		if _, err := os.ReadDir(p); err == nil {
			found = append(found, "listed "+p)
		}
	}
	if err := os.WriteFile("/tmp/dmrhub-plugin-test", []byte("x"), 0o600); err == nil {
		found = append(found, "wrote /tmp")
	}
	if len(os.Environ()) > 0 {
		found = append(found, fmt.Sprintf("env %d", len(os.Environ())))
	}
	plugin.MainWith(&pluginv1.Hello{Name: "Files", Description: strings.Join(found, "; ")}, func(context.Context, *plugin.Conn) error {
		select {}
	})
}
