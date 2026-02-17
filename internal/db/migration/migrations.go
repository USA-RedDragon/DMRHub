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

func Migrate(db *gorm.DB, cfg *config.Config) error {
	m := gormigrate.New(db, gormigrate.DefaultOptions, migrations(db, cfg))

	if err := m.Migrate(); err != nil {
		return fmt.Errorf("could not migrate: %w", err)
	}

	return nil
}

func migrations(db *gorm.DB, cfg *config.Config) []*gormigrate.Migration {
	return []*gormigrate.Migration{
		repeater_radio_id_migration_202302242025(db, cfg),
		drop_join_table_migration_202311190252(db, cfg),
		fix_call_data_migration_202602100435(db, cfg),

		fix_call_data_migration_202602100457(db, cfg),
		add_repeater_type_column_migration_202506150100(db, cfg),
		add_simplex_repeater_column_migration_202602130100(db, cfg),
		split_location_migration_202602160100(db, cfg),
		fix_ipsc_repeater_data_migration_202602160200(db, cfg),
		add_peer_ip_port_migration_202602170100(db, cfg),
		add_nets_tables_migration_202602160300(db, cfg),
		add_net_showcase_column_migration_202602170200(db, cfg),
	}
}
