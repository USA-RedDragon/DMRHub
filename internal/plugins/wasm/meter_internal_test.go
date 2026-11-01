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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/stretchr/testify/assert"
)

// runFor makes the plugin of m run for run and then wait for wait, on the
// clock fake.
func runFor(m *meter, fake *clock.Fake, run, wait time.Duration) {
	fake.Advance(run)
	m.Before(context.Background(), nil, nil, nil, nil)
	fake.Advance(wait)
	m.resume()
}

// TestALateCheckAllowsTheShareOfTheWholeWindow checks the CPU of a plugin
// that runs 40% of the time, with a share of 50%, three windows after the
// last check, as a check that a loaded machine runs late. The plugin used
// less than its share of that time, so the check must not stop it; a
// plugin that ran all that time is over its share.
func TestALateCheckAllowsTheShareOfTheWholeWindow(t *testing.T) {
	t.Parallel()
	const window, share = time.Second, 0.5
	for _, tc := range []struct {
		name      string
		run, wait time.Duration
		over      bool
	}{
		{name: "under its share", run: 400 * time.Millisecond, wait: 600 * time.Millisecond, over: false},
		{name: "spins", run: time.Second, wait: 0, over: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fake := clock.NewFake(time.Unix(0, 0))
			m := newMeter(fake)
			m.start(fake.Now())
			for range 3 {
				runFor(m, fake, tc.run, tc.wait)
			}
			assert.Equal(t, tc.over, m.over(fake.Now(), window, share), "%v of CPU in three windows", 3*tc.run)
		})
	}
}
