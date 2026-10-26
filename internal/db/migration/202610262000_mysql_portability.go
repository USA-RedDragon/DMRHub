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

// MySQLCollation is the collation of the tables on MySQL and MariaDB. It
// compares strings by their bytes, as Postgres and SQLite do.
const MySQLCollation = "utf8mb4_bin"

const mysqlDialect = "mysql"

// mysql_portability_migration_202610262000 gives MySQL and MariaDB what
// the other databases have: tables that compare strings by their bytes,
// and at most one active net for each talkgroup. MySQL has no partial
// index, so the nets get a virtual column that holds the talkgroup of an
// active net and NULL otherwise, with a unique index on it.
func mysql_portability_migration_202610262000(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	return &gormigrate.Migration{
		ID: "202610262000",
		Migrate: func(tx *gorm.DB) error {
			if tx.Name() != mysqlDialect {
				return nil
			}
			var tables []string
			if err := tx.Raw("SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = DATABASE() AND TABLE_TYPE = 'BASE TABLE' AND TABLE_COLLATION <> ?", MySQLCollation).Scan(&tables).Error; err != nil {
				return fmt.Errorf("could not list the tables to collate: %w", err)
			}
			for _, table := range tables {
				if err := tx.Exec(fmt.Sprintf("ALTER TABLE `%s` CONVERT TO CHARACTER SET utf8mb4 COLLATE %s", table, MySQLCollation)).Error; err != nil {
					return fmt.Errorf("could not collate table %s: %w", table, err)
				}
			}
			if !tx.Migrator().HasTable("nets") || tx.Migrator().HasIndex("nets", "idx_nets_one_active_per_tg") {
				return nil
			}
			if !tx.Migrator().HasColumn("nets", "active_talkgroup_id") {
				if err := tx.Exec("ALTER TABLE nets ADD COLUMN active_talkgroup_id bigint unsigned AS (IF(active, talkgroup_id, NULL)) VIRTUAL").Error; err != nil {
					return fmt.Errorf("could not add the active talkgroup of the nets: %w", err)
				}
			}
			if err := tx.Exec("CREATE UNIQUE INDEX idx_nets_one_active_per_tg ON nets (active_talkgroup_id)").Error; err != nil {
				return fmt.Errorf("could not limit the talkgroups to one active net: %w", err)
			}
			return nil
		},
		Rollback: func(*gorm.DB) error {
			return nil
		},
	}
}
