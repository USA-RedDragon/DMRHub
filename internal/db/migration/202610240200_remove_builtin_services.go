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
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/go-gormigrate/gormigrate/v2"
	"gorm.io/gorm"
)

// The parrot that the hub seeded at 9990 before the parrot became a plugin.
const (
	seededParrot          = 9990
	seededParrotCallsign  = "Parrot"
	seededParrotTalkgroup = "DMRHub Parrot"
)

// remove_builtin_services_migration_202610240200 removes what the built-in
// parrot and location server left: the user and talkgroup 9990 that the
// hub seeded, when an admin did not make them their own, and the table of
// open location requests. The calls of the parrot stay, without the links
// to its user and talkgroup.
func remove_builtin_services_migration_202610240200(_ *gorm.DB, _ *config.Config) *gormigrate.Migration {
	type LocationRequest struct {
		RadioID         uint   `gorm:"primaryKey;autoIncrement:false"`
		ServiceID       uint   `gorm:"not null"`
		RequestID       []byte `gorm:"not null"`
		IntervalSeconds uint   `gorm:"not null"`
		SentAt          time.Time
		RepeaterID      uint  `gorm:"not null;default:0"`
		Slot            uint8 `gorm:"not null;default:1"`
	}

	return &gormigrate.Migration{
		ID: "202610240200",
		Migrate: func(tx *gorm.DB) error {
			m := tx.Migrator()
			if !m.HasTable("talkgroups") || !m.HasTable("users") {
				return nil
			}
			var tgs int64
			if err := tx.Table("talkgroups").Where("id = ? AND name = ?", seededParrot, seededParrotTalkgroup).Count(&tgs).Error; err != nil {
				return fmt.Errorf("could not find the parrot talkgroup: %w", err)
			}
			if tgs > 0 {
				for _, q := range []string{
					"UPDATE repeaters SET ts1_dynamic_talkgroup_id = NULL WHERE ts1_dynamic_talkgroup_id = ?",
					"UPDATE repeaters SET ts2_dynamic_talkgroup_id = NULL WHERE ts2_dynamic_talkgroup_id = ?",
					"DELETE FROM repeater_ts1_static_talkgroups WHERE talkgroup_id = ?",
					"DELETE FROM repeater_ts2_static_talkgroups WHERE talkgroup_id = ?",
					"DELETE FROM talkgroup_admins WHERE talkgroup_id = ?",
					"DELETE FROM talkgroup_ncos WHERE talkgroup_id = ?",
					"UPDATE calls SET is_to_talkgroup = false, to_talkgroup_id = NULL WHERE to_talkgroup_id = ?",
					"DELETE FROM talkgroups WHERE id = ?",
				} {
					if err := execIfTable(tx, q); err != nil {
						return fmt.Errorf("could not remove the parrot talkgroup: %w", err)
					}
				}
			}
			var users int64
			if err := tx.Table("users").Where("id = ? AND callsign = ? AND (username = '' OR username IS NULL)", seededParrot, seededParrotCallsign).Count(&users).Error; err != nil {
				return fmt.Errorf("could not find the parrot user: %w", err)
			}
			if users > 0 {
				for _, q := range []string{
					"UPDATE calls SET user_id = NULL WHERE user_id = ?",
					"UPDATE calls SET is_to_user = false, to_user_id = NULL WHERE to_user_id = ?",
					"DELETE FROM talkgroup_admins WHERE user_id = ?",
					"DELETE FROM talkgroup_ncos WHERE user_id = ?",
					"DELETE FROM users WHERE id = ?",
				} {
					if err := execIfTable(tx, q); err != nil {
						return fmt.Errorf("could not remove the parrot user: %w", err)
					}
				}
			}
			if err := m.DropTable(&LocationRequest{}); err != nil {
				return fmt.Errorf("could not drop the location requests: %w", err)
			}
			return nil
		},
		Rollback: func(tx *gorm.DB) error {
			if err := tx.Migrator().CreateTable(&LocationRequest{}); err != nil {
				return fmt.Errorf("could not create the location requests: %w", err)
			}
			return nil
		},
	}
}

// execIfTable runs q, an UPDATE or DELETE of the parrot, when its table
// exists.
func execIfTable(tx *gorm.DB, q string) error {
	fields := strings.Fields(q)
	table := fields[1]
	if fields[0] == "DELETE" {
		table = fields[2]
	}
	if !tx.Migrator().HasTable(table) {
		return nil
	}
	if err := tx.Exec(q, seededParrot).Error; err != nil {
		return fmt.Errorf("could not change %s: %w", table, err)
	}
	return nil
}
