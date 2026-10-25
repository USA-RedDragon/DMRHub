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
package models_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSavingAStaleUserKeepsTheNewPassword saves a copy of a user loaded
// before its password changed, as an admin who approves or suspends the
// user at that moment does. The old password must not come back.
func TestSavingAStaleUserKeepsTheNewPassword(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "a", Password: "old-hash"}).Error)
	var stale models.User
	require.NoError(t, database.First(&stale, 3191868).Error)
	require.NoError(t, models.SetPassword(database, 3191868, "new-hash", false))

	stale.Suspended = true
	require.NoError(t, database.Save(&stale).Error)
	var got models.User
	require.NoError(t, database.First(&got, 3191868).Error)
	assert.True(t, got.Suspended)
	assert.Equal(t, "new-hash", got.Password)
}
