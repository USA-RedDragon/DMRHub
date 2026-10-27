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

package repeaters_test

import (
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRepeaterAndHotspotIDChecksAreApart(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			for _, tc := range []struct {
				name               string
				repeater, hotspot  bool
				repeaterCode, spot int
			}{
				{"both on", true, true, http.StatusBadRequest, http.StatusBadRequest},
				{"repeater off", false, true, http.StatusOK, http.StatusBadRequest},
				{"hotspot off", true, false, http.StatusBadRequest, http.StatusOK},
			} {
				t.Run(tc.name, func(t *testing.T) {
					t.Parallel()
					router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
						be.Migrated().Setup(t, c)
						c.DMR.DisableRadioIDValidation = false
						c.DMR.RadioIDChecks = config.RadioIDChecks{RepeaterIDs: tc.repeater, HotspotIDs: tc.hotspot}
					})
					require.NoError(t, err)
					defer tdb.CloseDB()
					_, _, jar := testutils.CreateAndLoginUser(t, router, apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "owner", Password: "password"})

					resp, w := testutils.CreateRepeater(t, router, jar, apimodels.RepeaterPost{RadioID: 999998})
					assert.Equal(t, tc.repeaterCode, w.Code, resp.Error)
					if tc.repeater {
						assert.Equal(t, "Repeater ID does not match assigned callsign", resp.Error)
					}
					resp, w = testutils.CreateRepeater(t, router, jar, apimodels.RepeaterPost{RadioID: 312345601})
					assert.Equal(t, tc.spot, w.Code, resp.Error)
					if tc.hotspot {
						assert.Equal(t, "Hotspot ID must start with user ID", resp.Error)
					}
				})
			}
		})
	}
}
