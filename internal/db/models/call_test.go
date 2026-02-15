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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFindCallsReturnsError verifies that FindCalls propagates DB errors
// instead of silently swallowing them. We close the underlying sql.DB to
// force errors from GORM.
func TestFindCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	// Close the underlying connection to provoke errors.
	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	calls, err := models.FindCalls(database)
	assert.Error(t, err, "FindCalls should return an error when the DB is closed")
	assert.Nil(t, calls)
}

// TestCountCallsReturnsError verifies CountCalls propagates errors.
func TestCountCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	count, err := models.CountCalls(database)
	assert.Error(t, err, "CountCalls should return an error when the DB is closed")
	assert.Equal(t, 0, count)
}

// TestFindUserCallsReturnsError verifies FindUserCalls propagates errors.
func TestFindUserCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	calls, err := models.FindUserCalls(database, 1)
	assert.Error(t, err, "FindUserCalls should return an error when the DB is closed")
	assert.Nil(t, calls)
}

// TestCountUserCallsReturnsError verifies CountUserCalls propagates errors.
func TestCountUserCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	count, err := models.CountUserCalls(database, 1)
	assert.Error(t, err, "CountUserCalls should return an error when the DB is closed")
	assert.Equal(t, 0, count)
}

// TestFindRepeaterCallsReturnsError verifies FindRepeaterCalls propagates errors.
func TestFindRepeaterCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	calls, err := models.FindRepeaterCalls(database, 1)
	assert.Error(t, err, "FindRepeaterCalls should return an error when the DB is closed")
	assert.Nil(t, calls)
}

// TestFindTalkgroupCallsReturnsError verifies FindTalkgroupCalls propagates errors.
func TestFindTalkgroupCallsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	calls, err := models.FindTalkgroupCalls(database, 1)
	assert.Error(t, err, "FindTalkgroupCalls should return an error when the DB is closed")
	assert.Nil(t, calls)
}

// TestActiveCallExistsReturnsError verifies ActiveCallExists propagates errors.
func TestActiveCallExistsReturnsError(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())

	exists, err := models.ActiveCallExists(database, 1, 1, 1, false, true)
	assert.Error(t, err, "ActiveCallExists should return an error when the DB is closed")
	assert.False(t, exists)
}

// TestCallQueriesSucceedOnValidDB verifies zero-result queries return nil error.
func TestCallQueriesSucceedOnValidDB(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	calls, err := models.FindCalls(database)
	assert.NoError(t, err)
	assert.Empty(t, calls)

	count, err := models.CountCalls(database)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	calls, err = models.FindUserCalls(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, calls)

	count, err = models.CountUserCalls(database, 999)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	calls, err = models.FindRepeaterCalls(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, calls)

	count, err = models.CountRepeaterCalls(database, 999)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	calls, err = models.FindTalkgroupCalls(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, calls)

	count, err = models.CountTalkgroupCalls(database, 999)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)

	exists, err := models.ActiveCallExists(database, 1, 1, 1, false, true)
	assert.NoError(t, err)
	assert.False(t, exists)
}
