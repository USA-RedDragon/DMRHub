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
	"errors"
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// foreignKeyDB returns a database that checks its foreign keys, as
// PostgreSQL and MySQL do.
func foreignKeyDB(t *testing.T) *gorm.DB {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:unowned_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared", "_pragma=foreign_keys(1)"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return database
}

func TestARepeaterWithNoOwnerSavesAndWaitsForAnOwner(t *testing.T) {
	t.Parallel()
	database := foreignKeyDB(t)
	const id, owner = 311860, 3191868
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeModem}).Error)
	var null int64
	require.NoError(t, database.Model(&models.Repeater{}).Where("id = ? AND owner_id IS NULL", id).Count(&null).Error)
	require.EqualValues(t, 1, null, "a repeater with no owner has a NULL owner_id")

	r, err := models.FindRepeaterByID(database, id)
	require.NoError(t, err)
	require.Zero(t, r.OwnerID)
	r.Callsign = "KI5VMF"
	require.NoError(t, database.Save(&r).Error)

	list, err := models.ListUnassignedRepeaters(database)
	require.NoError(t, err)
	require.Len(t, list, 1)
	n, err := models.CountUnassignedRepeaters(database)
	require.NoError(t, err)
	require.Equal(t, 1, n)

	require.True(t, errors.Is(models.AssignRepeaterOwner(database, id+1, owner), gorm.ErrRecordNotFound))
	require.NoError(t, database.Create(&models.User{ID: owner, Callsign: "KI5VMF", Username: "ki5vmf", Approved: true}).Error)
	require.NoError(t, models.AssignRepeaterOwner(database, id, owner))
	r, err = models.FindRepeaterByID(database, id)
	require.NoError(t, err)
	require.EqualValues(t, owner, r.OwnerID)
	require.Equal(t, "KI5VMF", r.Callsign)
	require.True(t, errors.Is(models.AssignRepeaterOwner(database, id, owner), models.ErrRepeaterHasOwner))
	n, err = models.CountUnassignedRepeaters(database)
	require.NoError(t, err)
	require.Zero(t, n)
}
