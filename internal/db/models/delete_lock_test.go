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

// lockingBackends are the backends whose deletes can meet a row that
// another connection inserts meanwhile, each with the statement that makes
// such an insert give up at once on a lock.
func lockingBackends() []struct {
	backend testutils.Backend
	wait    string
} {
	return []struct {
		backend testutils.Backend
		wait    string
	}{
		{testutils.PostgresRedisBackend(), "SET LOCAL lock_timeout = '1ms'"},
		{testutils.MySQLBackend(), "SET SESSION innodb_lock_wait_timeout = 1"},
		{testutils.MariaDBBackend(), "SET SESSION innodb_lock_wait_timeout = 1"},
	}
}

// startCallBefore inserts call from another connection right before the
// first delete of a row of table, as the call tracker does for a call that
// starts during a delete, and reports whether it ran.
func startCallBefore(t *testing.T, database *gorm.DB, table, wait string, call models.Call) *bool {
	t.Helper()
	started := false
	require.NoError(t, database.Callback().Delete().Before("gorm:delete").Register("start_a_call_"+table, func(tx *gorm.DB) {
		if started || tx.Statement.Table != table {
			return
		}
		started = true
		_ = database.Session(&gorm.Session{NewDB: true}).Transaction(func(other *gorm.DB) error {
			if err := other.Exec(wait).Error; err != nil {
				return err
			}
			return other.Omit(clause.Associations, "CallData").Create(&call).Error
		})
	}))
	return &started
}

// TestDeleteUserWhileACallStarts starts a call row of a repeater of a user
// and one of the user from another connection right before the delete of
// the user removes the repeater and the user. The delete must not fail on
// the foreign keys of the new rows.
func TestDeleteUserWhileACallStarts(t *testing.T) {
	t.Parallel()
	for _, be := range lockingBackends() {
		t.Run(be.backend.Name, func(t *testing.T) {
			t.Parallel()
			database := backendDB(t, be.backend)
			const user, rpt = 3002068, 990371
			require.NoError(t, database.Create(&models.User{ID: user, Callsign: "N0CALL", Username: "n0call"}).Error)
			require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: rpt}, Type: models.RepeaterTypeMMDVM, OwnerID: user}).Error)
			userID, rptID := uint(user), uint(rpt)
			viaRepeater := startCallBefore(t, database, "repeaters", be.wait, models.Call{StreamID: 1, Kind: models.CallKindVoice, RepeaterID: &rptID})
			ofUser := startCallBefore(t, database, "users", be.wait, models.Call{StreamID: 2, Kind: models.CallKindVoice, UserID: &userID})

			require.NoError(t, models.DeleteUser(database, user))
			require.True(t, *viaRepeater)
			require.True(t, *ofUser)
		})
	}
}

// TestDeleteTalkgroupWhileACallStarts starts a call row to a talkgroup from
// another connection right before the delete of the talkgroup removes the
// talkgroup row. The delete must not fail on the foreign key of the new
// row, and must leave no call to the talkgroup.
func TestDeleteTalkgroupWhileACallStarts(t *testing.T) {
	t.Parallel()
	for _, be := range lockingBackends() {
		t.Run(be.backend.Name, func(t *testing.T) {
			t.Parallel()
			database := backendDB(t, be.backend)
			const tg = 3100
			require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "TG"}).Error)
			tgID := uint(tg)
			started := startCallBefore(t, database, "talkgroups", be.wait, models.Call{StreamID: 1, Kind: models.CallKindVoice, IsToTalkgroup: true, ToTalkgroupID: &tgID})

			_, err := models.DeleteTalkgroup(database, tg)
			require.NoError(t, err)
			require.True(t, *started)
			var left int64
			require.NoError(t, database.Model(&models.Talkgroup{}).Unscoped().Where("id = ?", tg).Count(&left).Error)
			require.Zero(t, left, "the talkgroup is left")
		})
	}
}
