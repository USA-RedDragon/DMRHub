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

package migration

import (
	"fmt"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// username_callsign_clashes_migration_202610271200 logs each username that
// is the callsign of another user in any case, once. A login by such a
// name logs in as the user with the username, so an admin should rename
// one of the two users. It changes no data.
func username_callsign_clashes_migration_202610271200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610271200",
		Migrate: func(tx *gorm.DB) error {
			if !tx.Migrator().HasTable("users") {
				return nil
			}
			var rows []struct {
				UserID         uint
				Username       string
				CallsignUserID uint
				Callsign       string
			}
			err := tx.Raw("SELECT u.id AS user_id, u.username, o.id AS callsign_user_id, o.callsign FROM users u JOIN users o ON UPPER(o.callsign) = UPPER(u.username) AND o.id <> u.id WHERE u.deleted_at IS NULL AND o.deleted_at IS NULL ORDER BY u.id, o.id").Scan(&rows).Error
			if err != nil {
				return fmt.Errorf("could not look for usernames that are the callsigns of other users: %w", err)
			}
			for _, r := range rows {
				slog.Warn("A username is the callsign of another user. Rename one of them", "username", r.Username, "userID", r.UserID, "callsign", r.Callsign, "callsignUserID", r.CallsignUserID)
			}
			return nil
		},
		Rollback: func(_ *gorm.DB) error {
			return nil
		},
	}
}
