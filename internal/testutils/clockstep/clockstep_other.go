//go:build !clockstep || !linux

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

package clockstep

import (
	"testing"
	"time"
)

// Step skips the test: only the builds of the clockstep tag on Linux step
// the wall clock.
func Step(t *testing.T, _ time.Duration) {
	t.Helper()
	t.Skip("steps the wall clock of the machine; build it with the clockstep tag and run it with hack/clockstep.sh")
}

// Main runs the tests of m.
func Main(m *testing.M) int {
	return m.Run()
}
