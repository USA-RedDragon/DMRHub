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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestACallAtTheBoundsOfARangeIsInIt asks for the calls of a range that
// starts and ends at the time a call started, with bounds that have more
// precision than the column: MySQL rounds the start of the call up to the
// millisecond, MariaDB truncates it, and PostgreSQL rounds it to the
// microsecond. The call must be in the range on every backend.
func TestACallAtTheBoundsOfARangeIsInIt(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			database := backendDB(t, be)
			const tg, user = uint(3100), uint(1000001)
			require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "USA"}).Error)
			require.NoError(t, database.Create(&models.User{ID: user, Callsign: "N0CALL", Username: "n0call"}).Error)
			at := time.Date(2026, time.November, 2, 12, 0, 0, 900500, time.UTC)
			userID, toTG := user, tg
			require.NoError(t, database.Create(&models.Call{
				StreamID: 1, StartTime: at, Kind: models.CallKindVoice, UserID: &userID, SourceID: user,
				GroupCall: true, DestinationID: tg, IsToTalkgroup: true, ToTalkgroupID: &toTG,
			}).Error)

			calls, err := models.FindTalkgroupCallsInTimeRange(database, tg, at, at)
			require.NoError(t, err)
			assert.Len(t, calls, 1, "the call that started at the bounds is not in the range")
			count, err := models.CountTalkgroupCallsInTimeRange(database, tg, at, at)
			require.NoError(t, err)
			assert.Equal(t, 1, count)
			users, err := models.CountUniqueUsersInTimeRange(database, tg, at, at)
			require.NoError(t, err)
			assert.Equal(t, 1, users)
		})
	}
}
