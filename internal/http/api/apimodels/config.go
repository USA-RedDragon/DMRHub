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
	Secret       *string `json:"secret,omitempty"`
	PasswordSalt *string `json:"password-salt,omitempty"`
	HIBPAPIKey   *string `json:"hibp-api-key,omitempty"`
	SMTP         struct {
		config.SMTP
		Password *string `json:"password,omitempty"`
	}
	Database struct {
		config.Database
		Password *string `json:"password,omitempty"`
	}
	Redis struct {
		config.Redis
		Password *string `json:"password,omitempty"`
	}
}

type SecretStatus struct {
	SecretSet       bool `json:"secretSet"`
	PasswordSaltSet bool `json:"passwordSaltSet"`
	SMTPPasswordSet bool `json:"smtpPasswordSet"`
}

type ConfigResponse struct {
	config.Config
	Secrets SecretStatus `json:"secrets"`
}

func (p POSTConfig) ToConfig(c *config.Config, fallback *config.Config) {
	*c = p.Config
	if fallback != nil {
		c.Secret = fallback.Secret
		c.PasswordSalt = fallback.PasswordSalt
		c.HIBPAPIKey = fallback.HIBPAPIKey
		c.SMTP.Password = fallback.SMTP.Password
		c.Database.Password = fallback.Database.Password
		c.Redis.Password = fallback.Redis.Password
	}
	if p.Secret != nil {
		c.Secret = *p.Secret
	}
	if p.PasswordSalt != nil {
		c.PasswordSalt = *p.PasswordSalt
	}
	if p.HIBPAPIKey != nil {
		c.HIBPAPIKey = *p.HIBPAPIKey
	}
	if p.SMTP.Password != nil {
		c.SMTP.Password = *p.SMTP.Password
	}
	if p.Database.Password != nil {
		c.Database.Password = *p.Database.Password
	}
	if p.Redis.Password != nil {
		c.Redis.Password = *p.Redis.Password
	}
}
