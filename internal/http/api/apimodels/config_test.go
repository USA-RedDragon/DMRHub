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

package apimodels_test

import (
	"encoding/json"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPOSTConfigSetsPasswords(t *testing.T) {
	t.Parallel()
	body := `{
		"database": {"driver": "postgres", "host": "db", "password": "dbpass"},
		"redis": {"enabled": true, "host": "kv", "password": "kvpass"},
		"smtp": {"enabled": true, "host": "mail", "password": "mailpass"}
	}`
	var req apimodels.POSTConfig
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	var cfg config.Config
	req.ToConfig(&cfg, &config.Config{})
	assert.Equal(t, "postgres", string(cfg.Database.Driver))
	assert.Equal(t, "db", cfg.Database.Host)
	assert.Equal(t, "dbpass", cfg.Database.Password)
	assert.True(t, cfg.Redis.Enabled)
	assert.Equal(t, "kv", cfg.Redis.Host)
	assert.Equal(t, "kvpass", cfg.Redis.Password)
	assert.True(t, cfg.SMTP.Enabled)
	assert.Equal(t, "mail", cfg.SMTP.Host)
	assert.Equal(t, "mailpass", cfg.SMTP.Password)
}

func TestPOSTConfigKeepsPasswords(t *testing.T) {
	t.Parallel()
	body := `{"database": {"driver": "postgres", "host": "db"}, "redis": {"host": "kv"}, "smtp": {"host": "mail"}}`
	var req apimodels.POSTConfig
	require.NoError(t, json.Unmarshal([]byte(body), &req))
	current := &config.Config{}
	current.Database.Password = "olddb"
	current.Redis.Password = "oldkv"
	current.SMTP.Password = "oldmail"
	var cfg config.Config
	req.ToConfig(&cfg, current)
	assert.Equal(t, "db", cfg.Database.Host)
	assert.Equal(t, "olddb", cfg.Database.Password)
	assert.Equal(t, "oldkv", cfg.Redis.Password)
	assert.Equal(t, "oldmail", cfg.SMTP.Password)
}
