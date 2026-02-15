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

package auth_test

import (
	"math"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestSysadminLogin(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, _ := testutils.LoginAdmin(t, router)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)
}

func TestLogout(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	resp, w, adminJar := testutils.LoginAdmin(t, router)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged in", resp.Message)

	resp, w = testutils.LogoutUser(t, router, adminJar)

	assert.Equal(t, 200, w.Code)
	assert.Empty(t, resp.Error)
	assert.Equal(t, "Logged out", resp.Message)
}

// TestLoginTimingOracle verifies that login attempts for non-existent users
// take approximately the same time as login attempts for existing users with
// wrong passwords. This prevents attackers from determining whether a username
// exists based on response timing.
func TestLoginTimingOracle(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	const iterations = 5

	// Warm up: trigger the login timing calibration and JIT before
	// measuring, so the first real measurement isn't an outlier.
	testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: "warmup",
		Password: "warmup",
	})
	testutils.LoginUser(t, router, apimodels.AuthLogin{
		Username: "Admin",
		Password: "wrongpassword",
	})

	// Interleave measurements to eliminate ordering bias from CPU cache,
	// memory allocation patterns, or OS scheduler effects.
	existingUserDurations := make([]time.Duration, 0, iterations)
	nonExistentUserDurations := make([]time.Duration, 0, iterations)

	for i := 0; i < iterations; i++ {
		// Existing user, wrong password
		start := time.Now()
		resp, w, _ := testutils.LoginUser(t, router, apimodels.AuthLogin{
			Username: "Admin",
			Password: "wrongpassword",
		})
		existingUserDurations = append(existingUserDurations, time.Since(start))

		assert.Equal(t, 401, w.Code)
		assert.Equal(t, "Authentication failed", resp.Error)

		// Non-existent user
		start = time.Now()
		resp, w, _ = testutils.LoginUser(t, router, apimodels.AuthLogin{
			Username: "nonexistentuser",
			Password: "wrongpassword",
		})
		nonExistentUserDurations = append(nonExistentUserDurations, time.Since(start))

		assert.Equal(t, 401, w.Code)
		assert.Equal(t, "Authentication failed", resp.Error)
	}

	// Calculate average durations
	avgExisting := avgDuration(existingUserDurations)
	avgNonExistent := avgDuration(nonExistentUserDurations)

	// The non-existent user login should take at least 90% of the existing
	// user login time. The handler normalizes execution time with a
	// calibrated sleep floor, so both paths should be very close.
	ratio := float64(avgNonExistent) / float64(avgExisting)
	t.Logf("Average existing user (wrong pw): %v", avgExisting)
	t.Logf("Average non-existent user: %v", avgNonExistent)
	t.Logf("Ratio (non-existent/existing): %.2f", ratio)

	assert.Greater(t, ratio, 0.9, "Non-existent user login should take similar time to existing user login (timing oracle protection)")
}

func avgDuration(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	var sum float64
	for _, d := range durations {
		sum += float64(d)
	}
	return time.Duration(math.Round(sum / float64(len(durations))))
}
