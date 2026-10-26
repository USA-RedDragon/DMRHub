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

package main

import "testing"

func TestVersionAtLeastReadsTheTinyGoVersion(t *testing.T) {
	t.Parallel()
	for version, want := range map[string]bool{
		"tinygo version 0.43.0 linux/amd64 (using go version go1.27.2 and LLVM version 22.1.4)\n": true,
		"tinygo version 0.43.1 linux/amd64":  true,
		"tinygo version 0.44.0 linux/amd64":  true,
		"tinygo version 1.0.0 linux/amd64":   true,
		"tinygo version 0.42.9 linux/amd64":  false,
		"tinygo version 0.41.1 linux/amd64":  false,
		"tinygo version 0.100.0 linux/amd64": true,
		"go version go1.27.3 linux/amd64":    false,
		"":                                   false,
	} {
		if got := versionAtLeast(version, minTinyGo); got != want {
			t.Errorf("versionAtLeast(%q) = %v, want %v", version, got, want)
		}
	}
}
