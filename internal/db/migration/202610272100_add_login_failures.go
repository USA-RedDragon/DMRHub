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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// add_login_failures_migration_202610272100 adds the failed logins that the
// login backoff counts.
func add_login_failures_migration_202610272100(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type LoginFailure struct {
		ID      uint      `gorm:"primaryKey"`
		Account string    `gorm:"size:80;not null;index:idx_login_failures_account_at,priority:1"`
		At      time.Time `gorm:"not null;index;index:idx_login_failures_account_at,priority:2"`
	}

	return &gormigrate.Migration{
		ID: "202610272100",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if m.HasTable(&LoginFailure{}) {
				return nil
			}
			if err := m.CreateTable(&LoginFailure{}); err != nil {
				return fmt.Errorf("could not create the login_failures table: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().DropTable(&LoginFailure{}); err != nil {
				return fmt.Errorf("could not drop the login_failures table: %w", err)
			}
			return nil
		},
	}
}
