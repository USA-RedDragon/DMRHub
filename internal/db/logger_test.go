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
package db_test

import (
	"bytes"
	"errors"
	"log"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMakeDBLogsNoMissingRecord(t *testing.T) { //nolint:paralleltest // it swaps the log writer of MakeDB
	var out bytes.Buffer
	t.Cleanup(db.SetGormLogWriter(log.New(&out, "", 0)))
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = ""
	cfg.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	out.Reset()

	_, err = models.FindTalkgroupByID(database, 3000001)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound), err)
	_, err = models.FindRepeaterByID(database, 3000001)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound), err)
	_, err = models.FindUserByID(database, 3000001)
	require.True(t, errors.Is(err, gorm.ErrRecordNotFound), err)
	assert.Empty(t, out.String())

	require.Error(t, database.Exec("SELECT * FROM no_such_table").Error)
	assert.Contains(t, out.String(), "no_such_table")
}
