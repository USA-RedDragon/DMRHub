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

//go:build linux

package wasm

import (
	"bytes"
	"os"
	"runtime"
	"strconv"
	"syscall"
	"time"
)

// pinThread keeps the calling goroutine, which runs a plugin, on its OS
// thread, and returns the thread's ID, so that threadCPU can read the CPU
// time of the plugin.
func pinThread() int {
	runtime.LockOSThread()
	return syscall.Gettid()
}

// threadCPU returns the CPU time of the thread tid of this process, and
// false when the kernel does not tell it.
func threadCPU(tid int) (time.Duration, bool) {
	b, err := os.ReadFile("/proc/self/task/" + strconv.Itoa(tid) + "/schedstat")
	if err != nil {
		return 0, false
	}
	field, _, _ := bytes.Cut(bytes.TrimSpace(b), []byte(" "))
	ns, err := strconv.ParseInt(string(field), 10, 64)
	if err != nil {
		return 0, false
	}
	return time.Duration(ns), true
}
