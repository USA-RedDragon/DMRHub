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
package migration_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/migration"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDeleteDeletedUserAccess migrates a database with the sessions, app
// grants, password reset links and positions of a deleted user. Only the
// ones of the users that still exist must stay.
func TestDeleteDeletedUserAccess(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			memdb.Use(&cfg)
			be.Setup(t, &cfg)
			database, err := db.MakeDB(&cfg)
			require.NoError(t, err)
			t.Cleanup(func() {
				if sqlDB, err := database.DB(); err == nil {
					_ = sqlDB.Close()
				}
			})

			const kept, deleted = uint(3191868), uint(3140001)
			require.NoError(t, database.Exec("DELETE FROM migrations WHERE id = ?", "202610251010").Error)
			require.NoError(t, database.Create(&models.User{ID: kept, Callsign: "KI5VMF", Username: "kept", Approved: true}).Error)
			now := time.Now()
			for i, user := range []uint{kept, deleted} {
				key := fmt.Sprint(user)
				require.NoError(t, database.Create(&models.Session{ID: key, ExpiresAt: now.Add(time.Hour), UserID: &user}).Error)
				require.NoError(t, database.Create(&models.OAuthGrant{ID: key, UserID: user, ClientID: "app", CreatedAt: now, LastUsedAt: now}).Error)
				require.NoError(t, database.Create(&models.OAuthRefreshToken{Hash: key, GrantID: key, ExpiresAt: now.Add(time.Hour)}).Error)
				require.NoError(t, database.Create(&models.OAuthCode{Hash: key, UserID: user, ClientID: "app", ExpiresAt: now.Add(time.Minute)}).Error)
				require.NoError(t, database.Create(&models.PasswordReset{UserID: user, TokenHash: []byte(fmt.Sprintf("%032d", user)), ExpiresAt: now.Add(time.Hour), CreatedAt: now}).Error)
				require.NoError(t, database.Create(&models.PositionSetting{RadioID: user, UserID: user, Tracking: true}).Error)
				require.NoError(t, database.Create(&models.Position{ID: uint(i + 1), RadioID: user, Format: models.PositionFormatNMEA, ReceivedAt: now}).Error)
			}

			require.NoError(t, migration.Migrate(database, &cfg))

			for table, column := range map[string]string{
				"sessions": "user_id", "o_auth_grants": "user_id", "o_auth_codes": "user_id", "password_resets": "user_id",
				"position_settings": "user_id", "positions": "radio_id", "o_auth_refresh_tokens": "grant_id",
			} {
				var ids []string
				require.NoError(t, database.Raw(fmt.Sprintf("SELECT %s FROM %s ORDER BY 1", column, table)).Scan(&ids).Error, table)
				assert.Equal(t, []string{fmt.Sprint(kept)}, ids, table)
			}
		})
	}
}
