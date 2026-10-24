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

package config_test

import (
	"encoding/json"
	"net"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	configcontroller "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/config"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func transcodingStatus(t *testing.T, w int, body []byte) configcontroller.TranscodingStatus {
	t.Helper()
	require.Equal(t, http.StatusOK, w)
	var s configcontroller.TranscodingStatus
	require.NoError(t, json.Unmarshal(body, &s))
	return s
}

// TestGETTranscoding shows each AMBE server, its limit, how long ago it
// answered and the calls that hold it: no server when transcoding is off,
// the time of the last answer once a server answers, and no answer for a
// server that does not.
func TestGETTranscoding(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	_, _, jar := testutils.LoginAdmin(t, router)

	w := serve(t, router, http.MethodGet, "/api/v1/config/transcoding", nil, &jar)
	s := transcodingStatus(t, w.Code, w.Body.Bytes())
	assert.Empty(t, s.Servers)
	assert.Contains(t, w.Body.String(), `"servers":[]`)

	srv := testutils.StartFakeAMBEServer(t)
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	quiet := conn.LocalAddr().String()
	require.NoError(t, conn.Close())
	tdb.Hub().SetAMBEServers([]transcode.ServerConfig{{Addr: srv.Addr()}, {Addr: quiet, Calls: 1}})
	require.Eventually(t, func() bool {
		w := serve(t, router, http.MethodGet, "/api/v1/config/transcoding", nil, &jar)
		s = transcodingStatus(t, w.Code, w.Body.Bytes())
		return len(s.Servers) == 2 && s.Servers[0].AnsweredSeconds != nil
	}, 5*time.Second, 20*time.Millisecond)
	assert.Equal(t, srv.Addr(), s.Servers[0].Server)
	assert.Zero(t, s.Servers[0].CallsLimit)
	assert.Zero(t, *s.Servers[0].AnsweredSeconds)
	assert.Zero(t, s.Servers[0].Calls)
	assert.Equal(t, quiet, s.Servers[1].Server)
	assert.Equal(t, 1, s.Servers[1].CallsLimit)
	assert.Nil(t, s.Servers[1].AnsweredSeconds, "a server that does not answer shows no answer")
}
