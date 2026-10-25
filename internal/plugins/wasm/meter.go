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

import (
	"context"
	"slices"
	"sync"
	"time"

	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// meter measures the CPU time of a plugin: the CPU time of the thread that
// runs it, where the kernel tells it, and else all the time it does not
// wait in poll_oneoff, where a wasip1 program sleeps and waits for its
// stdin.
type meter struct {
	mu      sync.Mutex
	tid     int
	cpu     time.Duration
	waiting bool
	since   time.Time
	busy    time.Duration
	window  time.Time
}

func newMeter(now time.Time) *meter {
	return &meter{since: now, window: now}
}

// start starts measuring at now, when the plugin said hello. Its start
// takes the time it takes, within the limit of its hello.
func (m *meter) start(now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waiting, m.since, m.busy, m.window = false, now, 0, now
	if cpu, ok := threadCPU(m.tid); ok && m.tid != 0 {
		m.cpu = cpu
	}
}

// pin notes the thread tid that runs the plugin.
func (m *meter) pin(tid int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tid = tid
}

// NewFunctionListener listens to poll_oneoff of WASI.
func (m *meter) NewFunctionListener(def api.FunctionDefinition) experimental.FunctionListener {
	if slices.Contains(def.ExportNames(), "poll_oneoff") {
		return m
	}
	return nil
}

// Before notes that the plugin waits.
func (m *meter) Before(context.Context, api.Module, api.FunctionDefinition, []uint64, experimental.StackIterator) {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.waiting {
		m.busy += now.Sub(m.since)
	}
	m.waiting, m.since = true, now
}

// After notes that the plugin runs again.
func (m *meter) After(context.Context, api.Module, api.FunctionDefinition, []uint64) {
	m.resume()
}

// Abort notes that the plugin runs again.
func (m *meter) Abort(context.Context, api.Module, api.FunctionDefinition, error) {
	m.resume()
}

func (m *meter) resume() {
	now := time.Now()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.waiting, m.since = false, now
}

// over reports whether the plugin ran more than share of window in the
// window that holds now, and starts a new window when this one ended.
func (m *meter) over(now time.Time, window time.Duration, share float64) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	busy := m.busy
	if !m.waiting {
		busy += now.Sub(m.since)
	}
	cpu, onCPU := time.Duration(0), false
	if m.tid != 0 {
		cpu, onCPU = threadCPU(m.tid)
	}
	if onCPU {
		busy = cpu - m.cpu
	}
	if busy > time.Duration(float64(window)*share) {
		return true
	}
	if now.Sub(m.window) >= window {
		m.busy, m.window, m.cpu = 0, now, cpu
		if !m.waiting {
			m.since = now
		}
	}
	return false
}
