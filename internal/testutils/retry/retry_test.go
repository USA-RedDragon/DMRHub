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

package retry

import (
	"slices"
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	t.Parallel()

	Retry(t, 5, time.Millisecond, func(r *R) {
		if r.Attempt == 2 {
			return
		}
		r.Fail()
	})
}

func TestRetryAttempts(t *testing.T) {
	t.Parallel()

	var attempts int
	Retry(t, 10, time.Millisecond, func(r *R) {
		r.Logf("This line should appear only once.")
		r.Logf("attempt=%d", r.Attempt)
		attempts = r.Attempt

		// Retry 5 times.
		if r.Attempt == 5 {
			return
		}
		r.Fail()
	})

	if attempts != 5 {
		t.Errorf("attempts=%d; want %d", attempts, 5)
	}
}

type fakeTB struct {
	failed bool
}

func (f *fakeTB) Helper()             {}
func (f *fakeTB) Logf(string, ...any) {}
func (f *fakeTB) Fail()               { f.failed = true }

func TestBackoffDelays(t *testing.T) {
	t.Parallel()

	var delays []time.Duration
	tb := &fakeTB{}
	b := Backoff{
		Attempts: 5,
		Initial:  time.Second,
		Max:      3 * time.Second,
		Sleep:    func(d time.Duration) { delays = append(delays, d) },
	}
	calls := 0
	if b.Run(tb, func(r *R) { calls++; r.Fail() }) {
		t.Fatal("Run reported success for a function that always fails")
	}
	if !tb.failed {
		t.Error("Run did not mark the test as failed")
	}
	if calls != 5 {
		t.Errorf("calls=%d; want 5", calls)
	}
	want := []time.Duration{time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second}
	if !slices.Equal(delays, want) {
		t.Errorf("delays=%v; want %v", delays, want)
	}
}

func TestBackoffStopsOnSuccess(t *testing.T) {
	t.Parallel()

	var delays []time.Duration
	tb := &fakeTB{}
	b := Backoff{
		Attempts: 5,
		Initial:  time.Second,
		Sleep:    func(d time.Duration) { delays = append(delays, d) },
	}
	if !b.Run(tb, func(r *R) {
		if r.Attempt < 3 {
			r.Fail()
		}
	}) {
		t.Fatal("Run reported failure")
	}
	if tb.failed {
		t.Error("Run marked the test as failed")
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if !slices.Equal(delays, want) {
		t.Errorf("delays=%v; want %v", delays, want)
	}
}
