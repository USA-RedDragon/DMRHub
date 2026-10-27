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

package models

// GeneratedSecret is a secret that DMRHub made at its first start because
// no config file, environment variable or flag set it, such as the session
// secret. Every replica uses the same one, and an API never shows it.
type GeneratedSecret struct {
	Name  string `gorm:"primaryKey;size:64"`
	Value string `gorm:"type:text;not null"`
}

func (GeneratedSecret) TableName() string {
	return "generated_secrets"
}
