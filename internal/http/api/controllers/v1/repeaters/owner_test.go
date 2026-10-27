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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

func TestAnAdminAssignsTheOwnerOfAnUnassignedRepeater(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	database := tdb.DB()
	const id = 311860
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}, Type: models.RepeaterTypeModem}).Error)
	owner := testutils.SeedAccount(t, database, 3191868, "KI5VMF", "ki5vmf")
	userJar := testutils.Login(t, router, "ki5vmf")
	_, _, adminJar := testutils.LoginAdmin(t, router)

	require.Equal(t, http.StatusForbidden, testutils.Do(t, router, userJar, http.MethodGet, "/api/v1/repeaters/unassigned", nil).Code)
	require.Equal(t, http.StatusForbidden, testutils.Do(t, router, userJar, http.MethodPost, "/api/v1/repeaters/311860/owner", map[string]uint{"owner": owner.ID}).Code)

	w := testutils.Do(t, router, adminJar, http.MethodGet, "/api/v1/repeaters/unassigned", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list struct {
		Total     int `json:"total"`
		Repeaters []struct {
			ID   uint   `json:"id"`
			Type string `json:"type"`
		} `json:"repeaters"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Equal(t, 1, list.Total)
	require.Len(t, list.Repeaters, 1)
	require.EqualValues(t, id, list.Repeaters[0].ID)
	require.Equal(t, models.RepeaterTypeModem, list.Repeaters[0].Type)

	for _, c := range []struct {
		path string
		body any
		code int
	}{
		{"/api/v1/repeaters/311860/owner", map[string]uint{"owner": 1234567}, http.StatusBadRequest},
		{"/api/v1/repeaters/311860/owner", map[string]string{"owner": "x"}, http.StatusBadRequest},
		{"/api/v1/repeaters/311861/owner", map[string]uint{"owner": owner.ID}, http.StatusNotFound},
		{"/api/v1/repeaters/abc/owner", map[string]uint{"owner": owner.ID}, http.StatusBadRequest},
	} {
		w := testutils.Do(t, router, adminJar, http.MethodPost, c.path, c.body)
		require.Equal(t, c.code, w.Code, "%s %v: %s", c.path, c.body, w.Body.String())
	}

	w = testutils.Do(t, router, adminJar, http.MethodPost, "/api/v1/repeaters/311860/owner", map[string]uint{"owner": owner.ID})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	r, err := models.FindRepeaterByID(database, id)
	require.NoError(t, err)
	require.Equal(t, owner.ID, r.OwnerID)

	w = testutils.Do(t, router, adminJar, http.MethodPost, "/api/v1/repeaters/311860/owner", map[string]uint{"owner": owner.ID})
	require.Equal(t, http.StatusConflict, w.Code, w.Body.String())
	w = testutils.Do(t, router, adminJar, http.MethodGet, "/api/v1/repeaters/unassigned", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	require.Zero(t, list.Total)
	require.Empty(t, list.Repeaters)
	w = testutils.Do(t, router, userJar, http.MethodGet, "/api/v1/repeaters/my", nil)
	require.Equal(t, http.StatusOK, w.Code)
	require.Contains(t, w.Body.String(), `"id":311860`)
}

func TestAnAdminDeletesAnUnassignedRepeater(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	require.NoError(t, tdb.DB().Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311860}, Type: models.RepeaterTypeModem}).Error)
	_, _, adminJar := testutils.LoginAdmin(t, router)
	w := testutils.Do(t, router, adminJar, http.MethodDelete, "/api/v1/repeaters/311860", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	exists, err := models.RepeaterIDExists(tdb.DB(), 311860)
	require.NoError(t, err)
	require.False(t, exists)
}
