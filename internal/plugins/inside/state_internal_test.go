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

package inside

import (
	"errors"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/stretchr/testify/assert"
)

func TestTheReasonOfAStopIsASentence(t *testing.T) {
	t.Parallel()
	l := wasm.DefaultLimits()
	for want, got := range map[string]string{
		"It used more memory than a plugin may (64 MiB).": reasonOf(wasm.ErrMemory, "", l),
		"It used more than half a CPU core for 10 s.":     reasonOf(wasm.ErrCPU, "", l),
		"It did not answer DMRHub for 30 s.":              reasonOf(wasm.ErrNoAnswer, "", l),
		"It crashed: cannot reach the weather service.":   reasonOf(errors.New("the plugin failed: exit_code(1)"), "cannot reach the weather service", l),
		"It crashed: panic: no config.":                   reasonOf(&wasm.EndedError{Err: errors.New("exit_code(2)"), Said: "panic: no config"}, "", l),
		"It crashed: the plugin failed: exit_code(1).":    reasonOf(errors.New("the plugin failed: exit_code(1)"), "", l),
		"It crashed: it ended.":                           reasonOf(nil, "", l),
		"It used more than 25% of a CPU core for 10 s.":   reasonOf(wasm.ErrCPU, "", wasm.Limits{CPUShare: 0.25, CPUWindow: 10 * time.Second}),
		"It used more memory than a plugin may (32 MiB).": reasonOf(wasm.ErrMemory, "", wasm.Limits{MemoryPages: 512}),
	} {
		assert.Equal(t, want, got)
	}
}
