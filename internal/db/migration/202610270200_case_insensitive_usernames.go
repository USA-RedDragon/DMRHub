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
	"errors"
	"fmt"
	"strings"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// ErrUsernameClash is the error of the migration to case-insensitive
// usernames when some usernames differ only by case. An admin must rename
// or delete all but one user of each group first.
var ErrUsernameClash = errors.New("usernames differ only by case")

const usernameIndex = "idx_users_username_ci"

// case_insensitive_usernames_migration_202610270200 makes usernames unique
// in any case. It refuses while some usernames differ only by case, and
// names them, so that an admin decides which to keep. Postgres and SQLite
// get a unique index on LOWER(username). MySQL and MariaDB get a virtual
// column of LOWER(username) with a unique index, as MariaDB has no index
// on an expression.
func case_insensitive_usernames_migration_202610270200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610270200",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("users") || m.HasIndex("users", usernameIndex) {
				return nil
			}
			if err := usernameClashes(tx); err != nil {
				return err
			}
			if tx.Name() != mysqlDialect {
				if err := tx.Exec("CREATE UNIQUE INDEX " + usernameIndex + " ON users (LOWER(username))").Error; err != nil {
					return fmt.Errorf("could not make usernames unique in any case: %w", err)
				}
				return nil
			}
			if !m.HasColumn("users", "username_ci") {
				var columnType string
				if err := tx.Raw("SELECT COLUMN_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = DATABASE() AND TABLE_NAME = 'users' AND COLUMN_NAME = 'username'").Scan(&columnType).Error; err != nil {
					return fmt.Errorf("could not read the type of the usernames: %w", err)
				}
				if columnType == "" {
					return errors.New("could not read the type of the usernames")
				}
				if err := tx.Exec("ALTER TABLE users ADD COLUMN username_ci " + columnType + " AS (LOWER(username)) VIRTUAL").Error; err != nil {
					return fmt.Errorf("could not add the lower-case usernames: %w", err)
				}
			}
			if err := tx.Exec("CREATE UNIQUE INDEX " + usernameIndex + " ON users (username_ci)").Error; err != nil {
				return fmt.Errorf("could not make usernames unique in any case: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasIndex("users", usernameIndex) {
				if err := m.DropIndex("users", usernameIndex); err != nil {
					return fmt.Errorf("could not drop %s: %w", usernameIndex, err)
				}
			}
			if tx.Name() == mysqlDialect && m.HasColumn("users", "username_ci") {
				if err := m.DropColumn("users", "username_ci"); err != nil {
					return fmt.Errorf("could not drop the lower-case usernames: %w", err)
				}
			}
			return nil
		},
	}
}

func usernameClashes(tx *gorm.DB) error {
	var rows []struct {
		ID       uint
		Username string
	}
	err := tx.Raw("SELECT u.id, u.username FROM users u WHERE EXISTS (SELECT 1 FROM users o WHERE LOWER(o.username) = LOWER(u.username) AND o.id <> u.id) ORDER BY LOWER(u.username), u.id").Scan(&rows).Error
	if err != nil {
		return fmt.Errorf("could not look for usernames that differ only by case: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	var groups []string
	var group []string
	for i, r := range rows {
		group = append(group, fmt.Sprintf("%q (user %d)", r.Username, r.ID))
		if i == len(rows)-1 || !strings.EqualFold(rows[i+1].Username, r.Username) {
			groups = append(groups, strings.Join(group, ", "))
			group = nil
		}
	}
	return fmt.Errorf("%w: %s. Usernames are now the same in any case. Rename or delete all but one user of each group, then start DMRHub again", ErrUsernameClash, strings.Join(groups, "; "))
}
