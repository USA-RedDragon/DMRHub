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

// Package monotime keeps a time in an int64 with its monotonic clock

// Package bubble waits in the virtual time of a synctest bubble. A test
// that runs in synctest.Test calls these in place of the waits of testify,
// which check their condition in a goroutine of their own that can
// outlive the test.
package bubble

import (
	"fmt"
	"testing"
	"time"
)

// Eventually checks cond every tick until it is true, and fails the test
// when it is still false after within. In a bubble the time is virtual, so
// within bounds the time of the product, not the load of the machine.
func Eventually(t testing.TB, cond func() bool, within, tick time.Duration, msgAndArgs ...any) {
	t.Helper()
	deadline := time.Now().Add(within)
	for !cond() {
		if !time.Now().Before(deadline) {
			t.Fatalf("the condition was still false after %v%s", within, message(msgAndArgs))
		}
		time.Sleep(tick)
	}
}

// Receive returns the next value of ch, and fails the test when none comes
// within within.
func Receive[T any](t testing.TB, ch <-chan T, within time.Duration, msgAndArgs ...any) T {
	t.Helper()
	timer := time.NewTimer(within)
	defer timer.Stop()
	select {
	case v := <-ch:
		return v
	case <-timer.C:
		t.Fatalf("nothing came in %v%s", within, message(msgAndArgs))
		var zero T
		return zero
	}
}

func message(msgAndArgs []any) string {
	if len(msgAndArgs) == 0 {
		return ""
	}
	if f, ok := msgAndArgs[0].(string); ok {
		return ": " + fmt.Sprintf(f, msgAndArgs[1:]...)
	}
	return ": " + fmt.Sprint(msgAndArgs...)
}
