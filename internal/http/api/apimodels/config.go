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
package apimodels

import (
	"github.com/USA-RedDragon/DMRHub/internal/config"
)

type POSTConfig struct {
	config.Config
	Secret       string `json:"secret,omitempty"`
	PasswordSalt string `json:"password-salt,omitempty"`
	HIBPAPIKey   string `json:"hibp-api-key,omitempty"`
	SMTP         struct {
		config.SMTP
		Password string `json:"password,omitempty"`
	}
	Database struct {
		config.Database
		Password string `json:"password,omitempty"`
	}
	Redis struct {
		config.Redis
		Password string `json:"password,omitempty"`
	}
}

func (p POSTConfig) ToConfig(c *config.Config) {
	*c = p.Config
	c.Secret = p.Secret
	c.PasswordSalt = p.PasswordSalt
	c.HIBPAPIKey = p.HIBPAPIKey
	c.SMTP.Password = p.SMTP.Password
	c.Database.Password = p.Database.Password
	c.Redis.Password = p.Redis.Password
}
