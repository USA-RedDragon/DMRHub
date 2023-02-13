// SPDX-License-Identifier: AGPL-3.0-or-later
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

package v1_test

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/sdk"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestPingRoute(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	// Convert ts (time.Now().Unix()) to int64
	var tsInt int64
	fmt.Sscanf(w.Body.String(), "%d", &tsInt)

	w = httptest.NewRecorder()

	time.Sleep(1 * time.Second)

	req, _ = http.NewRequest("GET", "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var tsInt2 int64
	fmt.Sscanf(w.Body.String(), "%d", &tsInt2)

	assert.Greater(t, tsInt2, tsInt)
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	router, tdb := testutils.CreateTestDBRouter()
	defer tdb.CloseRedis()
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/version", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	assert.Equal(t, fmt.Sprintf("%s-%s", sdk.Version, sdk.GitCommit), w.Body.String())
}
