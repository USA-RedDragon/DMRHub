// SPDX-License-Identifier: AGPL-3.0-only
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package users_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestRegisterBadUser(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	// Test invalid user
	user := apimodels.UserRegistration{
		DMRId:    0,
		Callsign: "",
		Username: "",
		Password: "",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "JSON data is invalid", resp.Error)
}

func TestRegisterBadData(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer([]byte("invalid json data")))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err := json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "JSON data is invalid", resp.Error)
}

func TestRegisterBadDMRId(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    1,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "DMR ID is not valid", resp.Error)
}

func TestRegisterBadCallsign(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "N0CALL",
		Username: "n0call",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "Callsign does not match DMR ID", resp.Error)
}

func TestRegisterLowercaseCallsign(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "ki5vmf",
		Username: "username",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)
}

func TestRegisterUppercaseCallsign(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)
}

func TestRegisterDuplicateUsername(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	w = httptest.NewRecorder()

	user = apimodels.UserRegistration{
		DMRId:    3140598,
		Callsign: "KP4DJT",
		Username: "username",
		Password: "password",
	}

	jsonBytes, err = json.Marshal(user)
	assert.NoError(t, err)

	req, _ = http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	resp = testutils.APIResponse{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "Username is already taken", resp.Error)
}

func TestRegisterDuplicateDMRID(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username",
		Password: "password",
	}

	jsonBytes, err := json.Marshal(user)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var resp testutils.APIResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Error)
	assert.NotEmpty(t, resp.Message)
	assert.Equal(t, "User created, please wait for admin approval", resp.Message)

	user = apimodels.UserRegistration{
		DMRId:    3191868,
		Callsign: "KI5VMF",
		Username: "username2",
		Password: "password",
	}

	jsonBytes, err = json.Marshal(user)
	assert.NoError(t, err)

	w = httptest.NewRecorder()

	req, _ = http.NewRequest("POST", "/api/v1/users", bytes.NewBuffer(jsonBytes))
	router.ServeHTTP(w, req)

	assert.Equal(t, 400, w.Code)
	assert.NotEmpty(t, w.Body.String())

	resp = testutils.APIResponse{}
	err = json.Unmarshal(w.Body.Bytes(), &resp)

	assert.NoError(t, err)
	assert.Empty(t, resp.Message)
	assert.NotEmpty(t, resp.Error)
	assert.Equal(t, "DMR ID is already registered", resp.Error)
}
