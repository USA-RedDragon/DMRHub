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

package wasm

import "time"

// StopWait is stopWait for the tests.
const StopWait = stopWait

// PackData is packData for the tests.
var PackData = packData //nolint:gochecknoglobals // for the tests

// Compiled returns how many modules e holds compiled.
func Compiled(e *Engine) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.modules)
}

// SameCode reports whether a and b run the same compiled module.
func SameCode(a, b *Instance) bool {
	return a.code == b.code
}

// KeepFor sets how long e keeps a module that no plugin runs.
func KeepFor(e *Engine, d time.Duration) {
	e.keep = d
}

// Waiting reports whether the meter of i saw it wait in poll_oneoff.
func Waiting(i *Instance) bool {
	i.meter.mu.Lock()
	defer i.meter.mu.Unlock()
	return i.meter.waiting
}

// GuestCPU returns the CPU time that the thread of the plugin of i used,
// and false when the kernel does not tell it.
func GuestCPU(i *Instance) (time.Duration, bool) {
	i.meter.mu.Lock()
	tid := i.meter.tid
	i.meter.mu.Unlock()
	if tid == 0 {
		return 0, false
	}
	return threadCPU(tid)
}

// MeteredCPU returns the CPU time that the thread of the plugin of i used
// since the meter of i began its window, and false when the kernel does not
// tell it.
func MeteredCPU(i *Instance) (time.Duration, bool) {
	i.meter.mu.Lock()
	tid, base := i.meter.tid, i.meter.cpu
	i.meter.mu.Unlock()
	if tid == 0 {
		return 0, false
	}
	cpu, ok := threadCPU(tid)
	return cpu - base, ok
}
