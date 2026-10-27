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
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
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

// ConfigResponse is the config that a replica runs with, without its
// secrets, and how each setting stands on the replicas.
type ConfigResponse struct {
	config.Config
	Secrets SecretStatus `json:"secrets"`
	liveconfig.Status
	// Wizard is the state of the setup wizard while DMRHub cannot reach
	// its database; nil otherwise.
	Wizard *WizardStatus `json:"wizard,omitempty"`
}

// WizardStatus is the phase of the setup wizard, one of
// liveconfig.PhaseDatabase, PhaseSetup and PhaseDone, with why DMRHub
// cannot reach its database in the database phase.
type WizardStatus struct {
	Phase string `json:"phase"`
	liveconfig.Problem
}

// ConfigSaveResponse is how the replicas put a save of the Setup settings
// into effect. ApplyErrors lists what each replica that answered could not
// put into effect: a server, or a setting that the replica cannot run
// with, in Field.
type ConfigSaveResponse struct {
	Message     string              `json:"message,omitempty"`
	Error       string              `json:"error,omitempty"`
	Revision    uint64              `json:"revision"`
	Replicas    []ConfigSaveReplica `json:"replicas"`
	ApplyErrors []ConfigApplyError  `json:"applyErrors"`
}

// ConfigSaveReplica is the report of one replica on a save. Answered is
// false for a replica that did not report in time.
type ConfigSaveReplica struct {
	ID          string                  `json:"id"`
	Self        bool                    `json:"self"`
	Answered    bool                    `json:"answered"`
	Revision    uint64                  `json:"revision"`
	ApplyErrors []liveconfig.ApplyError `json:"apply_errors"`
	Invalid     []liveconfig.FieldError `json:"invalid"`
}

// ConfigApplyError is one thing that a replica could not put into effect.
type ConfigApplyError struct {
	Replica string `json:"replica"`
	Server  string `json:"server"`
	Field   string `json:"field,omitempty"`
	Running bool   `json:"running"`
	Error   string `json:"error"`
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
