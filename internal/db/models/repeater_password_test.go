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

// TestAStaleSaveKeepsTheRotatedPassword saves a copy of a repeater read
// before its password was rotated, as an edit that ran at the same time
// would: the new password must stay.
func TestAStaleSaveKeepsTheRotatedPassword(t *testing.T) {
	t.Parallel()
	database := foreignKeyDB(t)
	const id = 319186801
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeMMDVM, Password: "oldPassword1"}).Error)
	stale, err := models.FindRepeaterByID(database, id)
	require.NoError(t, err)
	require.NoError(t, models.SetRepeaterPassword(database, id, "newPassword2"))
	stale.SimplexRepeater = true
	require.NoError(t, database.Save(&stale).Error)
	saved, err := models.FindRepeaterByID(database, id)
	require.NoError(t, err)
	require.Equal(t, "newPassword2", saved.Password)
	require.True(t, saved.SimplexRepeater)
	require.ErrorIs(t, models.SetRepeaterPassword(database, 311860, "x"), gorm.ErrRecordNotFound)
}
