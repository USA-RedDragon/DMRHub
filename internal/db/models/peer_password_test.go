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
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAStaleSaveKeepsTheRotatedPeerPassword saves a copy of a peer read
// before its password was rotated, as an edit that ran at the same time
// would: the new password must stay. The rotation moves the generation of
// the peer, so the OpenBridge servers forget the link version of the old
// password.
func TestAStaleSaveKeepsTheRotatedPeerPassword(t *testing.T) {
	t.Parallel()
	database := foreignKeyDB(t)
	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "ki5vmf"}).Error)
	const id = 3000
	require.NoError(t, database.Create(&models.Peer{ID: id, OwnerID: 3191868, Password: "oldPassword1!", IP: "192.0.2.10", Port: 62035}).Error)
	stale, err := models.FindPeerByID(database, id)
	require.NoError(t, err)
	require.NoError(t, models.SetPeerPassword(database, id, "newPassword2!"))
	rotated, err := models.FindPeerByID(database, id)
	require.NoError(t, err)
	require.Equal(t, "newPassword2!", rotated.Password)
	require.True(t, rotated.UpdatedAt.After(stale.UpdatedAt), "the rotation must move the generation of the peer")
	stale.Port = 62036
	require.NoError(t, database.Omit("Owner").Save(&stale).Error)
	saved, err := models.FindPeerByID(database, id)
	require.NoError(t, err)
	require.Equal(t, "newPassword2!", saved.Password)
	require.Equal(t, 62036, saved.Port)
	require.ErrorIs(t, models.SetPeerPassword(database, 3001, "x"), gorm.ErrRecordNotFound)
}
