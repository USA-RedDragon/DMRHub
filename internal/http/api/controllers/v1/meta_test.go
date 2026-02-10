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

package v1_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

const testTimeout = 1 * time.Minute

func TestPingRoute(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/ping", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	// Convert ts (time.Now().Unix()) to int64
	var tsInt int64
	_, err = fmt.Sscanf(w.Body.String(), "%d", &tsInt)
	assert.NoError(t, err)

	w = httptest.NewRecorder()

	time.Sleep(1 * time.Second)

	ctx, cancel2 := context.WithTimeout(context.Background(), testTimeout)
	defer cancel2()

	req, err = http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/ping", nil)
	assert.NoError(t, err)

	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())

	var tsInt2 int64
	_, err = fmt.Sscanf(w.Body.String(), "%d", &tsInt2)
	assert.NoError(t, err)

	assert.Greater(t, tsInt2, tsInt)
}

func TestVersionRoute(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/version", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.NotEmpty(t, w.Body.String())
}
