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
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/require"
)

// secretLog records the repeaters whose secret change the hub announced.
type secretLog struct {
	mu  sync.Mutex
	ids []uint
}

func (l *secretLog) add(id uint) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, id)
}

func (l *secretLog) list() []uint {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]uint(nil), l.ids...)
}

func passwordOf(t *testing.T, tdb *testutils.TestDB, id uint) string {
	t.Helper()
	r, err := models.FindRepeaterByID(tdb.DB(), id)
	require.NoError(t, err)
	return r.Password
}

func rotated(t *testing.T, body []byte) apimodels.RepeaterRotatePasswordResponse {
	t.Helper()
	var res apimodels.RepeaterRotatePasswordResponse
	require.NoError(t, json.Unmarshal(body, &res))
	return res
}

// TestAnOwnerRotatesThePasswordOfTheirRepeater rotates the password of a
// hotspot and the auth key of an IPSC repeater as their owner: each gets a
// new secret of its kind, the old one is gone from the database, and the
// hub tells every replica to end the sessions of the repeater.
func TestAnOwnerRotatesThePasswordOfTheirRepeater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		router, tdb, err := testutils.CreateTestDBRouter()
		require.NoError(t, err)
		defer tdb.CloseDB()
		database := tdb.DB()
		owner := testutils.SeedAccount(t, database, 3002068, "N6CALL", "n6call")
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300206801}, Type: models.RepeaterTypeMMDVM, Password: "oldPassword1", OwnerID: owner.ID}).Error)
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 990360}, Type: models.RepeaterTypeIPSC, Password: "0000000000000000000000000000000000003039", OwnerID: owner.ID}).Error)
		var announced secretLog
		defer tdb.Hub().OnRepeaterSecretChange(announced.add)()
		jar := testutils.Login(t, router, "n6call")

		before := time.Now()
		w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/repeaters/300206801/rotate-password", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.Equal(t, map[string]bool{"password": true, "rotated_at": true}, testutils.JSONKeys(t, w.Body.Bytes()))
		res := rotated(t, w.Body.Bytes())
		require.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9]{12}$`), res.Password)
		require.NotEqual(t, "oldPassword1", res.Password)
		require.Equal(t, res.Password, passwordOf(t, tdb, 300206801))
		require.False(t, res.RotatedAt.Before(before.Truncate(time.Second)))

		w = testutils.Do(t, router, jar, http.MethodPost, "/api/v1/repeaters/990360/rotate-password", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		key := rotated(t, w.Body.Bytes())
		require.Regexp(t, regexp.MustCompile(`^[0-9a-f]{40}$`), key.Password)
		require.Equal(t, key.Password, passwordOf(t, tdb, 990360))

		bubble.SettleDB(t, tdb.DB(), time.Minute)
		require.Len(t, announced.list(), 2)
		require.Equal(t, []uint{300206801, 990360}, announced.list())
	})
}

// TestOnlyTheOwnerOrAnAdminRotatesAPassword checks who may rotate the
// password of a repeater: another user and a visitor may not, and an admin
// may.
func TestOnlyTheOwnerOrAnAdminRotatesAPassword(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		router, tdb, err := testutils.CreateTestDBRouter()
		require.NoError(t, err)
		defer tdb.CloseDB()
		database := tdb.DB()
		owner := testutils.SeedAccount(t, database, 3002068, "N6CALL", "n6call")
		testutils.SeedAccount(t, database, 3002069, "N0CALL", "other")
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300206801}, Type: models.RepeaterTypeMMDVM, Password: "oldPassword1", OwnerID: owner.ID}).Error)
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 990361}, Type: models.RepeaterTypeModem}).Error)
		var announced secretLog
		defer tdb.Hub().OnRepeaterSecretChange(announced.add)()
		const path = "/api/v1/repeaters/300206801/rotate-password"

		w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, path, nil)
		require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
		w = testutils.Do(t, router, testutils.Login(t, router, "other"), http.MethodPost, path, nil)
		require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
		require.Equal(t, "oldPassword1", passwordOf(t, tdb, 300206801))
		bubble.SettleDB(t, tdb.DB(), time.Minute)
		require.Empty(t, announced.list())

		_, _, adminJar := testutils.LoginAdmin(t, router)
		w = testutils.Do(t, router, adminJar, http.MethodPost, path, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		require.NotEqual(t, "oldPassword1", passwordOf(t, tdb, 300206801))

		for _, c := range []struct {
			path string
			code int
		}{
			{"/api/v1/repeaters/311862/rotate-password", http.StatusNotFound},
			{"/api/v1/repeaters/990361/rotate-password", http.StatusBadRequest},
			{"/api/v1/repeaters/abc/rotate-password", http.StatusBadRequest},
		} {
			w := testutils.Do(t, router, adminJar, http.MethodPost, c.path, nil)
			require.Equal(t, c.code, w.Code, "%s: %s", c.path, w.Body.String())
		}
		bubble.SettleDB(t, tdb.DB(), time.Minute)
		require.Len(t, announced.list(), 1)
	})
}
