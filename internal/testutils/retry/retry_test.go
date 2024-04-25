// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"testing"
	"time"
)

func TestRetry(t *testing.T) {
	Retry(t, 5, time.Millisecond, func(r *R) {
		if r.Attempt == 2 {
			return
		}
		r.Fail()
	})
}

func TestRetryAttempts(t *testing.T) {
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
