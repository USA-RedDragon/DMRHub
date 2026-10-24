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
	"context"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const transcodingSection = manager.Kind("transcoding")

// TestLiveAMBEServerChange sets the AMBE servers with the config API, as
// the Setup page does. The hub uses them at once, refuses an address
// without a port, and turns transcoding off for an empty list.
func TestLiveAMBEServerChange(t *testing.T) { //nolint:paralleltest // t.Setenv for the saved config file
	useTempConfigDir(t)
	l := testutils.SetupLiveStack(t)
	assert.False(t, l.Hub.Transcoder().Enabled(), "transcoding is off by default")

	one, two := testutils.StartFakeAMBEServer(t), testutils.StartFakeAMBEServer(t)
	list := []any{map[string]any{"address": one.Addr(), "calls": 0}, map[string]any{"address": two.Addr(), "calls": 1}}
	w := l.SetDMR(t, transcodingSection, map[string]any{"ambe-servers": list})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	want := []transcode.ServerConfig{{Addr: one.Addr()}, {Addr: two.Addr(), Calls: 1}}
	assert.Equal(t, want, ambeServersOf(l))

	w = l.SetDMR(t, transcodingSection, map[string]any{"ambe-servers": []any{map[string]any{"address": "ambeserver"}}})
	require.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
	assert.Contains(t, w.Body.String(), "ambe-servers")
	assert.Equal(t, want, ambeServersOf(l), "a refused list leaves the servers as they were")

	w = l.SetDMR(t, transcodingSection, map[string]any{"ambe-servers": []any{}})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, l.Hub.Transcoder().Enabled())
}

func ambeServersOf(l *testutils.LiveStack) []transcode.ServerConfig {
	status := l.Hub.Transcoder().Status(context.Background())
	out := make([]transcode.ServerConfig, 0, len(status))
	for _, st := range status {
		out = append(out, st.ServerConfig)
	}
	return out
}
