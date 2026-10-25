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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// delete_deleted_user_access_migration_202610251010 deletes the sessions,
// app grants, password reset links and tracked positions that a deleted
// user left, so that a new user with the same DMR ID does not get them.
func delete_deleted_user_access_migration_202610251010(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	const gone = "user_id IS NOT NULL AND NOT EXISTS (SELECT 1 FROM users WHERE users.id = user_id)"
	return &gormigrate.Migration{
		ID: "202610251010",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("users") {
				return nil
			}
			steps := []struct{ table, query string }{
				{"positions", "DELETE FROM positions WHERE radio_id IN (SELECT radio_id FROM position_settings WHERE " + gone + ")"},
				{"o_auth_refresh_tokens", "DELETE FROM o_auth_refresh_tokens WHERE grant_id IN (SELECT id FROM o_auth_grants WHERE " + gone + ")"},
				{"position_settings", "DELETE FROM position_settings WHERE " + gone},
				{"o_auth_grants", "DELETE FROM o_auth_grants WHERE " + gone},
				{"o_auth_codes", "DELETE FROM o_auth_codes WHERE " + gone},
				{"password_resets", "DELETE FROM password_resets WHERE " + gone},
				{"sessions", "DELETE FROM sessions WHERE " + gone},
			}
			for _, step := range steps {
				if !m.HasTable(step.table) {
					continue
				}
				if err := tx.Exec(step.query).Error; err != nil {
					return fmt.Errorf("could not delete the rows of deleted users from %s: %w", step.table, err)
				}
			}
			return nil
		},
		Rollback: func(*gorm.DB) error {
			return nil
		},
	}
}
