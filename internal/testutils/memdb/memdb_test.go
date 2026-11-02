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

package memdb_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

// TestAReadDoesNotWaitForAWriteOfItsTable reads a repeater, as a login
// does, while a transaction that wrote the repeaters table is open, as a
// ping does. A server on a SQLite file in WAL mode answers the read from
// the last commit. A shared-cache database locks the table until the
// transaction ends, so the read never returns.
func TestAReadDoesNotWaitForAWriteOfItsTable(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	gdb, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	sqlDB, err := gdb.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	require.NoError(t, gdb.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Approved: true}).Error)
	require.NoError(t, gdb.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001, Callsign: "T0001ST"},
		OwnerID:               1000001,
		Type:                  models.RepeaterTypeMMDVM,
		Password:              "password",
	}).Error)

	tx := gdb.Begin()
	require.NoError(t, tx.Error)
	require.NoError(t, tx.Model(&models.Repeater{}).Where("id = ?", 100001).UpdateColumn("last_ping", time.Unix(0, 0)).Error)
	exists, err := models.RepeaterIDExists(gdb, 100001)
	require.NoError(t, err)
	require.True(t, exists)
	r, err := models.FindRepeaterByID(gdb, 100001)
	require.NoError(t, err)
	require.Equal(t, "password", r.Password)
	require.NoError(t, tx.Commit().Error)
}
