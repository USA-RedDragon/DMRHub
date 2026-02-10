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
	"testing"

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
