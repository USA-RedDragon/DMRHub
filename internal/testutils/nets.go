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

package testutils

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SeedNet creates an active net in the database for integration testing.
func (s *IntegrationStack) SeedNet(t *testing.T, talkgroupID, startedByUserID uint, description string) models.Net {
	t.Helper()
	net := models.Net{
		TalkgroupID:     talkgroupID,
		StartedByUserID: startedByUserID,
		StartTime:       time.Now(),
		Description:     description,
		Active:          true,
	}
	err := s.DB.Create(&net).Error
	require.NoError(t, err)
	return net
}

// SeedEndedNet creates an ended net in the database for integration testing.
func (s *IntegrationStack) SeedEndedNet(t *testing.T, talkgroupID, startedByUserID uint, description string, duration time.Duration) models.Net {
	t.Helper()
	startTime := time.Now().Add(-duration)
	endTime := time.Now()
	net := models.Net{
		TalkgroupID:     talkgroupID,
		StartedByUserID: startedByUserID,
		StartTime:       startTime,
		EndTime:         &endTime,
		Description:     description,
		Active:          false,
	}
	err := s.DB.Create(&net).Error
	require.NoError(t, err)
	return net
}

// SeedScheduledNet creates a scheduled net in the database for integration testing.
func (s *IntegrationStack) SeedScheduledNet(t *testing.T, talkgroupID, createdByUserID uint, name string, dayOfWeek int, timeOfDay, timezone string) models.ScheduledNet {
	t.Helper()
	cronExpr, err := models.GenerateCronExpression(dayOfWeek, timeOfDay)
	require.NoError(t, err)
	sn := models.ScheduledNet{
		TalkgroupID:     talkgroupID,
		CreatedByUserID: createdByUserID,
		Name:            name,
		CronExpression:  cronExpr,
		DayOfWeek:       dayOfWeek,
		TimeOfDay:       timeOfDay,
		Timezone:        timezone,
		Enabled:         true,
	}
	err = s.DB.Create(&sn).Error
	require.NoError(t, err)
	return sn
}

// StartNet starts a new net via the API.
func StartNet(t *testing.T, router *gin.Engine, jar CookieJar, body apimodels.NetStartPost) (apimodels.NetResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(body)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/nets/start", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp apimodels.NetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

// StopNet stops an active net via the API.
func StopNet(t *testing.T, router *gin.Engine, jar CookieJar, netID uint) (apimodels.NetResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fmt.Sprintf("/api/v1/nets/%d/stop", netID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp apimodels.NetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

// GetNet retrieves a net by ID via the API.
func GetNet(t *testing.T, router *gin.Engine, jar CookieJar, netID uint) (apimodels.NetResponse, *httptest.ResponseRecorder) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/%d", netID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp apimodels.NetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

// ListNets lists nets via the API.
func ListNets(t *testing.T, router *gin.Engine, jar CookieJar, queryParams string) (*httptest.ResponseRecorder, []byte) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	url := "/api/v1/nets"
	if queryParams != "" {
		url += "?" + queryParams
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)
	return w, w.Body.Bytes()
}

// GetNetCheckIns retrieves check-ins for a net via the API.
func GetNetCheckIns(t *testing.T, router *gin.Engine, jar CookieJar, netID uint) (*httptest.ResponseRecorder, []byte) {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/%d/checkins", netID), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)
	return w, w.Body.Bytes()
}

// ExportNetCheckIns exports check-ins for a net via the API.
func ExportNetCheckIns(t *testing.T, router *gin.Engine, jar CookieJar, netID uint, format string) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/%d/checkins/export?format=%s", netID, format), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)
	return w
}

// CreateScheduledNet creates a scheduled net via the API.
func CreateScheduledNet(t *testing.T, router *gin.Engine, jar CookieJar, body apimodels.ScheduledNetPost) (apimodels.ScheduledNetResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(body)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/nets/scheduled", bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp apimodels.ScheduledNetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}

// DeleteScheduledNet deletes a scheduled net via the API.
func DeleteScheduledNet(t *testing.T, router *gin.Engine, jar CookieJar, id uint) *httptest.ResponseRecorder {
	t.Helper()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, fmt.Sprintf("/api/v1/nets/scheduled/%d", id), nil)
	assert.NoError(t, err)

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)
	return w
}

// PatchNet updates a net (e.g. showcase toggle) via the API.
func PatchNet(t *testing.T, router *gin.Engine, jar CookieJar, netID uint, body apimodels.NetPatch) (apimodels.NetResponse, *httptest.ResponseRecorder) {
	t.Helper()

	jsonBytes, err := json.Marshal(body)
	assert.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/nets/%d", netID), bytes.NewBuffer(jsonBytes))
	assert.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range jar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	var resp apimodels.NetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	assert.NoError(t, err)
	return resp, w
}
