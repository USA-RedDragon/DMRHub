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

package dmr_test

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetupSaveWritesNoConfigFile saves a change in Setup while the config
// directory is read-only, as a ConfigMap mount is. The save takes effect,
// and DMRHub writes no config file.
func TestSetupSaveWritesNoConfigFile(t *testing.T) { //nolint:paralleltest // t.Setenv for the config directory
	home := t.TempDir()
	readOnly := filepath.Join(home, "config")
	require.NoError(t, os.Mkdir(readOnly, 0o500))
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", readOnly)

	l := testutils.SetupLiveStack(t)
	w := l.SetDMR(t, manager.IPSC, map[string]any{"network-id": 888888})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.InDelta(t, 888888, l.DMRSetting(t, manager.IPSC, "network-id"), 0)

	entries, err := os.ReadDir(readOnly)
	require.NoError(t, err)
	assert.Empty(t, entries, "DMRHub must not write a config file")
}
