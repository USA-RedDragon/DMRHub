//go:build clockstep

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
	"os"
	"syscall"
	"testing"
	"time"
)

// Step moves the wall clock of the machine by d, and back by d when the
// test ends. It skips the test unless DMRHUB_CLOCK_STEP is 1, as in the VM
// of hack/clockstep.sh.
func Step(t *testing.T, d time.Duration) {
	t.Helper()
	if os.Getenv("DMRHUB_CLOCK_STEP") != "1" {
		t.Skip("steps the wall clock of the machine; run it with hack/clockstep.sh")
	}
	set := func(d time.Duration) {
		tv := syscall.NsecToTimeval(time.Now().Add(d).UnixNano())
		if err := syscall.Settimeofday(&tv); err != nil {
			t.Fatalf("stepping the wall clock: %v", err)
		}
	}
	before := time.Now()
	set(d)
	if moved := time.Now().Round(0).Sub(before.Round(0)); (moved - d).Abs() > time.Second {
		t.Fatalf("the wall clock moved %s, want %s", moved, d)
	}
	t.Cleanup(func() { set(-d) })
}
