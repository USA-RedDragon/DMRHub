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
	"encoding/json"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// POSTConfig is a config that the Setup page sends, with the secrets that
// an admin typed. A secret that it leaves out keeps its running value.
type POSTConfig struct {
	config.Config
	Secret           *string `json:"-"`
	PasswordSalt     *string `json:"-"`
	HIBPAPIKey       *string `json:"-"`
	SMTPPassword     *string `json:"-"`
	DatabasePassword *string `json:"-"`
	RedisPassword    *string `json:"-"`
}

type password struct {
	Password *string `json:"password"`
}

type postSecrets struct {
	Secret       *string  `json:"secret"`
	PasswordSalt *string  `json:"password-salt"`
	HIBPAPIKey   *string  `json:"hibp-api-key"`
	SMTP         password `json:"smtp"`
	Database     password `json:"database"`
	Redis        password `json:"redis"`
}

// UnmarshalJSON reads the config and its secrets, which config.Config
// never reads from JSON.
func (p *POSTConfig) UnmarshalJSON(b []byte) error {
	if err := json.Unmarshal(b, &p.Config); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	var s postSecrets
	if err := json.Unmarshal(b, &s); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}
	p.Secret, p.PasswordSalt, p.HIBPAPIKey = s.Secret, s.PasswordSalt, s.HIBPAPIKey
	p.SMTPPassword, p.DatabasePassword, p.RedisPassword = s.SMTP.Password, s.Database.Password, s.Redis.Password
	return nil
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

// ToConfig sets c to the config of p, with the secrets of fallback that p
// leaves out.
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
	if p.SMTPPassword != nil {
		c.SMTP.Password = *p.SMTPPassword
	}
	if p.DatabasePassword != nil {
		c.Database.Password = *p.DatabasePassword
	}
	if p.RedisPassword != nil {
		c.Redis.Password = *p.RedisPassword
	}
}
