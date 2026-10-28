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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/utils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func addHotspot(t *testing.T, r *testutils.LiveReplica, jar testutils.CookieJar, id uint) int {
	t.Helper()
	return testutils.Do(t, r.Router, jar, http.MethodPost, "/api/v1/repeaters", map[string]any{"id": id, "type": "mmdvm"}).Code
}

// TestRepeaterQuotaStopsAUserAtTheLimit saves a quota of 2 in Setup on one
// replica. A user then adds two hotspots on the other replica and not a
// third, and an admin has no limit.
func TestRepeaterQuotaStopsAUserAtTheLimit(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range allBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{})
			code, _, raw := putSetting(t, l.First(), map[string]any{"dmr.repeater-quota": 2})
			require.Equal(t, http.StatusOK, code, raw)

			hash, err := utils.HashPassword(testutils.TestPassword, l.Config.PasswordSalt)
			require.NoError(t, err)
			require.NoError(t, l.DB.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "ki5vmf", Password: hash, Approved: true}).Error)
			user := testutils.Login(t, b.Router, "ki5vmf")
			assert.Equal(t, http.StatusOK, addHotspot(t, b, user, 319186801))
			assert.Equal(t, http.StatusOK, addHotspot(t, b, user, 319186802))
			assert.Equal(t, http.StatusForbidden, addHotspot(t, b, user, 319186803), "a user stops at the quota")

			assert.Equal(t, http.StatusOK, addHotspot(t, b, b.Admin, 999990101))
			assert.Equal(t, http.StatusOK, addHotspot(t, b, b.Admin, 999990102))
			assert.Equal(t, http.StatusOK, addHotspot(t, b, b.Admin, 999990103), "an admin has no limit")

			code, _, raw = putSetting(t, l.First(), map[string]any{"dmr.repeater-quota": 0})
			require.Equal(t, http.StatusOK, code, raw)
			assert.Equal(t, http.StatusOK, addHotspot(t, b, user, 319186803), "a quota of 0 is no limit")
		})
	}
}
