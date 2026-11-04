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
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type hubAnswers func(*http.Request) (*http.Response, error)

func (f hubAnswers) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// pingRunner returns a runner of one repeater whose DMRHub API answers
// the last ping that lastPing gives for each request.
func pingRunner(cursor time.Time, lastPing func(n int) time.Time) *runner {
	r := newRunner(options{}, nil, nil, "http://hub", cursor)
	r.peers[300000401] = nil
	r.hub = &hubProcess{exited: make(chan struct{}), pingErrors: map[string]string{}}
	var n atomic.Int64
	r.apis["n0call"] = &api{base: "http://hub", client: &http.Client{Transport: hubAnswers(func(*http.Request) (*http.Response, error) {
		body := fmt.Sprintf(`{"last_ping_time": %q}`, lastPing(int(n.Add(1))).Format(time.RFC3339Nano))
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(body)), Header: http.Header{}}, nil
	})}}
	return r
}

// TestPingWaitOutlastsASlowSave has DMRHub save the last ping only after
// a minute, as a starved SQLite store can. The wait must end when the ping
// is saved, however long that takes.
func TestPingWaitOutlastsASlowSave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cursor := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		start := time.Now()
		r := pingRunner(cursor, func(int) time.Time {
			if time.Since(start) < time.Minute {
				return cursor.Add(-time.Minute)
			}
			return cursor
		})
		assert.NoError(t, r.waitPings(t.Context()))
		assert.GreaterOrEqual(t, time.Since(start), time.Minute)
	})
}

// TestPingWaitStopsAtAFailedSave has DMRHub log that it could not save
// the last ping. The wait must fail at once with that line, since no
// later save comes before the next ping.
func TestPingWaitStopsAtAFailedSave(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cursor := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		r := pingRunner(cursor, func(int) time.Time { return cursor.Add(-time.Minute) })
		r.hub.pingErrors["300000401"] = "ERR Error saving the last ping of a repeater repeaterID=300000401 error=\"database is locked\""
		start := time.Now()
		assert.ErrorContains(t, r.waitPings(t.Context()), "database is locked")
		assert.Less(t, time.Since(start), time.Second)
	})
}

// TestHubLogNamesTheRepeaterOfAFailedPingSave reads the log line that
// DMRHub writes when it could not save a last ping.
func TestHubLogNamesTheRepeaterOfAFailedPingSave(t *testing.T) {
	t.Parallel()
	m := pingError.FindStringSubmatch(`Oct 13 18:45:46.120 ERR Error saving the last ping of a repeater repeaterID=300000401 error="database is locked (5) (SQLITE_BUSY)"`)
	if assert.NotNil(t, m) {
		assert.Equal(t, "300000401", m[1])
	}
}

// TestATickHoldsTheClockStill moves the demo clock on by a shot step. The
// clock must stay at the new time until the next step: a clock that runs
// on in real time would stamp what a shot makes with the time the run
// took, which a loaded machine stretches.
func TestATickHoldsTheClockStill(t *testing.T) {
	t.Parallel()
	c, err := openClock(filepath.Join(t.TempDir(), "clock"))
	require.NoError(t, err)
	t.Cleanup(c.close)
	start := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
	r := newRunner(options{}, nil, c, "http://hub", start)
	require.NoError(t, r.tick(shotStep))
	assert.True(t, c.frozen())
	assert.True(t, c.now().Equal(start.Add(shotStep)))
}

// TestTheSavedPingWaitKeepsTheClockStill waits for the saved ping on a
// frozen clock. Its reads must not move the clock on, since how many it
// takes depends on the load of the machine.
func TestTheSavedPingWaitKeepsTheClockStill(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c, err := openClock(filepath.Join(t.TempDir(), "clock"))
		require.NoError(t, err)
		t.Cleanup(c.close)
		cursor := time.Date(2026, 10, 13, 18, 30, 0, 0, time.UTC)
		c.freeze(cursor)
		start := time.Now()
		r := pingRunner(cursor, func(int) time.Time {
			if time.Since(start) < time.Second {
				return cursor.Add(-time.Minute)
			}
			return cursor
		})
		r.c = c
		r.apis["n0call"].before = r.beforeRequest
		require.NoError(t, r.waitPings(t.Context()))
		assert.True(t, c.now().Equal(cursor))
	})
}
