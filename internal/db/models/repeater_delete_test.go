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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// TestDeleteRepeaterWhileACallStarts starts a call row of a repeater from
// another connection right before the delete of the repeater removes the
// repeater row, after it removed the calls of the repeater, as the call
// tracker does for a call that starts during the delete. The delete must
// not fail on the foreign key of the new call row.
func TestDeleteRepeaterWhileACallStarts(t *testing.T) {
	t.Parallel()
	for _, be := range []struct {
		backend testutils.Backend
		wait    string
	}{
		{testutils.PostgresRedisBackend(), "SET LOCAL lock_timeout = '1ms'"},
		{testutils.MySQLBackend(), "SET SESSION innodb_lock_wait_timeout = 1"},
		{testutils.MariaDBBackend(), "SET SESSION innodb_lock_wait_timeout = 1"},
	} {
		t.Run(be.backend.Name, func(t *testing.T) {
			t.Parallel()
			database := backendDB(t, be.backend)
			const id = 311870
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeMMDVM}).Error)

			started := false
			require.NoError(t, database.Callback().Delete().Before("gorm:delete").Register("start_a_call", func(tx *gorm.DB) {
				if started || tx.Statement.Table != "repeaters" {
					return
				}
				started = true
				rpt := uint(id)
				_ = database.Session(&gorm.Session{NewDB: true}).Transaction(func(other *gorm.DB) error {
					if err := other.Exec(be.wait).Error; err != nil {
						return err
					}
					return other.Omit(clause.Associations, "CallData").Create(&models.Call{StreamID: 1, Kind: models.CallKindVoice, RepeaterID: &rpt}).Error
				})
			}))

			require.NoError(t, models.DeleteRepeater(database, id))
			require.True(t, started)
			var left int64
			require.NoError(t, database.Model(&models.Call{}).Where("repeater_id = ?", id).Count(&left).Error)
			require.Zero(t, left, "a call row of the deleted repeater is left")
		})
	}
}
