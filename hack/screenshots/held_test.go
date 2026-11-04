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

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAHeldTextWaitsForTheSweep sends a text that store and forward holds
// only on its next sweep, a real-time timer, after AnswerWait of demo time
// with no answer. After the clock step the wait must end only once the
// database holds the text.
func TestAHeldTextWaitsForTheSweep(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := dynamicDB(t)
		require.NoError(t, db.Exec("CREATE TABLE held_messages (id integer PRIMARY KEY, radio_id integer)").Error)
		start := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		r := newRunner(options{}, nil, nil, "http://hub", start)
		r.expectHeld(3000011)
		r.cursor = start.Add(shotStep)
		go func() {
			time.Sleep(4 * time.Second)
			_ = db.Exec("INSERT INTO held_messages (radio_id) VALUES (3000011)").Error
		}()
		began := time.Now()
		require.NoError(t, r.waitHeld(t.Context(), db))
		assert.GreaterOrEqual(t, time.Since(began), 4*time.Second)
		assert.Empty(t, r.held)
	})
}

// TestAStepPastTheAnswerWindowFails moves the clock past the answer
// window of a text that DMRHub has not held. Store and forward then never
// holds it, so the shot must fail at once rather than show no held text.
func TestAStepPastTheAnswerWindowFails(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		db := dynamicDB(t)
		require.NoError(t, db.Exec("CREATE TABLE held_messages (id integer PRIMARY KEY, radio_id integer)").Error)
		start := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		r := newRunner(options{}, nil, nil, "http://hub", start)
		r.expectHeld(3000011)
		r.cursor = start.Add(answerWindow + time.Second)
		assert.ErrorContains(t, r.waitHeld(t.Context(), db), "answer window")
	})
}
