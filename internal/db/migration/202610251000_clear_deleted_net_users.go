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

// clear_deleted_net_users_migration_202610251000 clears the user of the
// nets and scheduled nets whose user was deleted, so that a new user with
// the same DMR ID does not become their user.
func clear_deleted_net_users_migration_202610251000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610251000",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("users") {
				return nil
			}
			for _, ref := range []struct{ table, column string }{
				{"nets", "started_by_user_id"},
				{"scheduled_nets", "created_by_user_id"},
			} {
				if !m.HasTable(ref.table) {
					continue
				}
				query := fmt.Sprintf("UPDATE %[1]s SET %[2]s = NULL WHERE %[2]s IS NOT NULL AND NOT EXISTS (SELECT 1 FROM users WHERE users.id = %[1]s.%[2]s)", ref.table, ref.column)
				if err := tx.Exec(query).Error; err != nil {
					return fmt.Errorf("could not clear the deleted users of %s: %w", ref.table, err)
				}
			}
			return nil
		},
		Rollback: func(*gorm.DB) error {
			return nil
		},
	}
}
