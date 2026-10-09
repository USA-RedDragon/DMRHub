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
	"bytes"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"time"
)

// R is passed to each run of a flaky test run, manages state and accumulates log statements.
type R struct {
	// The number of current attempt.
	Attempt int

	failed bool
	log    *bytes.Buffer
}

// Fail marks the run as failed, and will retry once the function returns.
func (r *R) Fail() {
	r.failed = true
}

// Errorf is equivalent to Logf followed by Fail.
func (r *R) Errorf(s string, v ...interface{}) {
	r.logf(s, v...)
	r.Fail()
}

// Logf formats its arguments and records it in the error log.
// The text is only printed for the final unsuccessful run or the first successful run.
func (r *R) Logf(s string, v ...interface{}) {
	r.logf(s, v...)
}

func (r *R) logf(s string, v ...interface{}) {
	fmt.Fprint(r.log, "\n")
	fmt.Fprint(r.log, lineNumber())
	fmt.Fprintf(r.log, s, v...)
}

func lineNumber() string {
	const skip = 3 // logf, public func, user function
	_, file, line, ok := runtime.Caller(skip)
	if !ok {
		return ""
	}
	return filepath.Base(file) + ":" + strconv.Itoa(line) + ": "
}

// TB is the part of testing.TB that Backoff reports to.
type TB interface {
	Helper()
	Logf(format string, args ...any)
	Fail()
}

// Backoff retries a function with an exponentially growing delay between attempts.
type Backoff struct {
	// Attempts is the maximum number of times the function runs.
	Attempts int
	// Initial is the delay after the first failed attempt. Each later delay doubles.
	Initial time.Duration
	// Max caps the delay. Zero means no cap.
	Max time.Duration
	// Sleep waits between attempts. It defaults to time.Sleep and lets tests skip the wait.
	Sleep func(time.Duration)
}

// Run calls f until it succeeds or the attempts run out, marking t as failed in the latter case.
func (b Backoff) Run(t TB, f func(r *R)) bool {
	t.Helper()
	sleep := b.Sleep
	if sleep == nil {
		sleep = time.Sleep
	}
	delay := b.Initial
	for attempt := 1; attempt <= b.Attempts; attempt++ {
		r := &R{Attempt: attempt, log: &bytes.Buffer{}}

		f(r)

		if !r.failed {
			if r.log.Len() != 0 {
				t.Logf("Success after %d attempts:%s", attempt, r.log.String())
			}
			return true
		}

		if attempt == b.Attempts {
			t.Logf("FAILED after %d attempts:%s", attempt, r.log.String())
			break
		}

		t.Logf("attempt %d failed, retrying in %s:%s", attempt, delay, r.log.String())
		sleep(delay)
		delay *= 2
		if b.Max > 0 && delay > b.Max {
			delay = b.Max
		}
	}
	t.Fail()
	return false
}

// Retry runs f up to maxAttempts times with a fixed delay between attempts.
func Retry(t TB, maxAttempts int, sleep time.Duration, f func(r *R)) bool {
	t.Helper()
	return Backoff{Attempts: maxAttempts, Initial: sleep, Max: sleep}.Run(t, f)
}
