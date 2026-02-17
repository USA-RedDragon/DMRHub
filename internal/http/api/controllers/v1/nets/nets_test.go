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

package nets_test

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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 1 * time.Minute

func TestGETNetsUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/nets", nil)
	require.NoError(t, err)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total int               `json:"total"`
		Nets  []json.RawMessage `json:"nets"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.Nets)
}

func TestPOSTNetStartAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create a talkgroup first
	tgResp, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Test Net TG",
		Description: "TG for net testing",
	})
	require.Equal(t, http.StatusOK, tgW.Code)
	assert.Empty(t, tgResp.Error)

	// Find the TG ID
	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Test Net TG").First(&tg).Error
	require.NoError(t, err)

	// Start a net
	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Test net session",
	}
	resp, w := testutils.StartNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, resp.Active)
	assert.Equal(t, tg.ID, resp.TalkgroupID)
	assert.Equal(t, "Test net session", resp.Description)
}

func TestPOSTNetStartUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	body := apimodels.NetStartPost{
		TalkgroupID: 1,
		Description: "Should fail",
	}
	jsonBytes, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/nets/start", bytes.NewBuffer(jsonBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPOSTNetStartDuplicateConflict(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Dup Net TG",
		Description: "TG for duplicate net test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Dup Net TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "First net",
	}

	// Start net 1
	_, w := testutils.StartNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusCreated, w.Code)

	// Start net 2 — should conflict
	body.Description = "Second net"
	_, w = testutils.StartNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestPOSTNetStartNonexistentTalkgroup(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	body := apimodels.NetStartPost{
		TalkgroupID: 999999,
		Description: "Net on missing TG",
	}
	_, w := testutils.StartNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPOSTNetStopAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Stop Net TG",
		Description: "TG for net stop test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Stop Net TG").First(&tg).Error
	require.NoError(t, err)

	// Start a net
	startBody := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Net to stop",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, startBody)
	require.Equal(t, http.StatusCreated, startW.Code)

	// Stop the net
	stopResp, stopW := testutils.StopNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, stopW.Code)
	assert.False(t, stopResp.Active)
	assert.NotNil(t, stopResp.EndTime)
}

func TestPOSTNetStopNonexistent(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, w := testutils.StopNet(t, router, adminJar, 99999)
	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGETNetByID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "GetNet TG",
		Description: "TG for get net test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "GetNet TG").First(&tg).Error
	require.NoError(t, err)

	startBody := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Get this net",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, startBody)
	require.Equal(t, http.StatusCreated, startW.Code)

	getResp, getW := testutils.GetNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, getW.Code)
	assert.Equal(t, startResp.ID, getResp.ID)
	assert.True(t, getResp.Active)
	assert.Equal(t, "Get this net", getResp.Description)
}

func TestGETNetByIDNotFound(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/nets/99999", nil)
	require.NoError(t, err)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestGETNetsFilterByTalkgroupID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Filter TG",
		Description: "TG for filter test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Filter TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Filtered net",
	}
	_, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	w, bodyBytes := testutils.ListNets(t, router, adminJar, fmt.Sprintf("talkgroup_id=%d", tg.ID))
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total int                     `json:"total"`
		Nets  []apimodels.NetResponse `json:"nets"`
	}
	err = json.Unmarshal(bodyBytes, &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Len(t, result.Nets, 1)
}

func TestGETNetsFilterActiveForTalkgroup(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Active Filter TG",
		Description: "TG for active filter test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Active Filter TG").First(&tg).Error
	require.NoError(t, err)

	// No active net yet
	w, bodyBytes := testutils.ListNets(t, router, adminJar, fmt.Sprintf("talkgroup_id=%d&active=true", tg.ID))
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total int                     `json:"total"`
		Nets  []apimodels.NetResponse `json:"nets"`
	}
	err = json.Unmarshal(bodyBytes, &result)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.Nets)

	// Start a net
	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Active net",
	}
	_, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	// Now should find it
	w, bodyBytes = testutils.ListNets(t, router, adminJar, fmt.Sprintf("talkgroup_id=%d&active=true", tg.ID))
	assert.Equal(t, http.StatusOK, w.Code)

	err = json.Unmarshal(bodyBytes, &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Len(t, result.Nets, 1)
	assert.True(t, result.Nets[0].Active)
}

func TestGETNetCheckInsEmpty(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "CheckIn TG",
		Description: "TG for check-in test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "CheckIn TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Check-in test net",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	w, bodyBytes := testutils.GetNetCheckIns(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total    int               `json:"total"`
		CheckIns []json.RawMessage `json:"check_ins"`
	}
	err = json.Unmarshal(bodyBytes, &result)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
	assert.Empty(t, result.CheckIns)
}

func TestGETNetCheckInsExportCSV(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "ExportCSV TG",
		Description: "TG for CSV export test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "ExportCSV TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "CSV export test",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	w := testutils.ExportNetCheckIns(t, router, adminJar, startResp.ID, "csv")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "text/csv")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
	// Check that the CSV header row is present
	assert.Contains(t, w.Body.String(), "Call ID")
}

func TestGETNetCheckInsExportJSON(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "ExportJSON TG",
		Description: "TG for JSON export test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "ExportJSON TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "JSON export test",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	w := testutils.ExportNetCheckIns(t, router, adminJar, startResp.ID, "json")
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Contains(t, w.Header().Get("Content-Type"), "application/json")
	assert.Contains(t, w.Header().Get("Content-Disposition"), "attachment")
}

func TestGETNetCheckInsExportInvalidFormat(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "ExportBad TG",
		Description: "TG for bad format test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "ExportBad TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Bad format test",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, startW.Code)

	w := testutils.ExportNetCheckIns(t, router, adminJar, startResp.ID, "xml")
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestNetStartStopLifecycle(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Lifecycle TG",
		Description: "TG for lifecycle test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Lifecycle TG").First(&tg).Error
	require.NoError(t, err)

	// Start net
	startBody := apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Lifecycle net",
	}
	startResp, startW := testutils.StartNet(t, router, adminJar, startBody)
	require.Equal(t, http.StatusCreated, startW.Code)
	assert.True(t, startResp.Active)

	// Verify via GET
	getResp, getW := testutils.GetNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, getW.Code)
	assert.True(t, getResp.Active)

	// Stop net
	stopResp, stopW := testutils.StopNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, stopW.Code)
	assert.False(t, stopResp.Active)
	assert.NotNil(t, stopResp.EndTime)

	// Verify via GET after stop
	getResp, getW = testutils.GetNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, getW.Code)
	assert.False(t, getResp.Active)
	assert.NotNil(t, getResp.EndTime)

	// Should be able to start a new net on the same TG now
	startBody.Description = "Second net"
	_, startW = testutils.StartNet(t, router, adminJar, startBody)
	assert.Equal(t, http.StatusCreated, startW.Code)
}

func TestPOSTNetStartWithDuration(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Duration TG",
		Description: "TG for duration test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Duration TG").First(&tg).Error
	require.NoError(t, err)

	dur := uint(60)
	body := apimodels.NetStartPost{
		TalkgroupID:     tg.ID,
		Description:     "Duration net",
		DurationMinutes: &dur,
	}
	resp, w := testutils.StartNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.True(t, resp.Active)
	assert.NotNil(t, resp.DurationMinutes)
	assert.Equal(t, uint(60), *resp.DurationMinutes)
}

// Scheduled Net Tests

func TestGETScheduledNetsUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/nets/scheduled", nil)
	require.NoError(t, err)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total         int               `json:"total"`
		ScheduledNets []json.RawMessage `json:"scheduled_nets"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 0, result.Total)
}

func TestPOSTScheduledNetAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Sched TG",
		Description: "TG for scheduled net test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Sched TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "Weekly Net",
		Description: "Test scheduled net",
		DayOfWeek:   3, // Wednesday
		TimeOfDay:   "19:00",
		Timezone:    "America/New_York",
	}
	resp, w := testutils.CreateScheduledNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusCreated, w.Code)
	assert.Equal(t, "Weekly Net", resp.Name)
	assert.True(t, resp.Enabled)
	assert.Equal(t, 3, resp.DayOfWeek)
	assert.Equal(t, "19:00", resp.TimeOfDay)
}

func TestPOSTScheduledNetUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	body := apimodels.ScheduledNetPost{
		TalkgroupID: 1,
		Name:        "Should fail",
		TimeOfDay:   "19:00",
		Timezone:    "UTC",
	}
	jsonBytes, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/nets/scheduled", bytes.NewBuffer(jsonBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPOSTScheduledNetInvalidTimeOfDay(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "BadTime TG",
		Description: "TG for bad time test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "BadTime TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "Bad Time Net",
		DayOfWeek:   1,
		TimeOfDay:   "invalid",
		Timezone:    "UTC",
	}
	_, w := testutils.CreateScheduledNet(t, router, adminJar, body)
	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPOSTScheduledNetEmptyName(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "EmptyName TG",
		Description: "TG for empty name test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "EmptyName TG").First(&tg).Error
	require.NoError(t, err)

	// binding:"required" means an empty Name in JSON will fail ShouldBindJSON
	body := map[string]interface{}{
		"talkgroup_id": tg.ID,
		"name":         "",
		"day_of_week":  1,
		"time_of_day":  "19:00",
		"timezone":     "UTC",
	}
	jsonBytes, err := json.Marshal(body)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "/api/v1/nets/scheduled", bytes.NewBuffer(jsonBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	// name is required, so empty string should fail bind or name-length check
	assert.True(t, w.Code == http.StatusBadRequest)
}

func TestDELETEScheduledNet(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "DeleteSched TG",
		Description: "TG for delete scheduled test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "DeleteSched TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "To Delete",
		DayOfWeek:   5,
		TimeOfDay:   "20:00",
		Timezone:    "UTC",
	}
	createResp, createW := testutils.CreateScheduledNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, createW.Code)

	w := testutils.DeleteScheduledNet(t, router, adminJar, createResp.ID)
	assert.Equal(t, http.StatusOK, w.Code)

	// Verify it's gone
	getW := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/scheduled/%d", createResp.ID), nil)
	require.NoError(t, err)
	router.ServeHTTP(getW, getReq)
	assert.Equal(t, http.StatusNotFound, getW.Code)
}

func TestGETScheduledNetByID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "GetSched TG",
		Description: "TG for get scheduled test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "GetSched TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "Get This Sched",
		DayOfWeek:   0,
		TimeOfDay:   "08:30",
		Timezone:    "UTC",
	}
	createResp, createW := testutils.CreateScheduledNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, createW.Code)

	getW := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	getReq, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/scheduled/%d", createResp.ID), nil)
	require.NoError(t, err)
	router.ServeHTTP(getW, getReq)

	assert.Equal(t, http.StatusOK, getW.Code)

	var resp apimodels.ScheduledNetResponse
	err = json.Unmarshal(getW.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Get This Sched", resp.Name)
	assert.Equal(t, 0, resp.DayOfWeek)
	assert.Equal(t, "08:30", resp.TimeOfDay)
}

func TestGETScheduledNetNotFound(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/nets/scheduled/99999", nil)
	require.NoError(t, err)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPATCHScheduledNet(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "PatchSched TG",
		Description: "TG for patch scheduled test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "PatchSched TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "Original Name",
		DayOfWeek:   2,
		TimeOfDay:   "14:00",
		Timezone:    "UTC",
	}
	createResp, createW := testutils.CreateScheduledNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, createW.Code)

	// Patch the name
	patchBody := apimodels.ScheduledNetPatch{
		Name: strPtr("Updated Name"),
	}
	jsonBytes, err := json.Marshal(patchBody)
	require.NoError(t, err)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/nets/scheduled/%d", createResp.ID), bytes.NewBuffer(jsonBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")

	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}

	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp apimodels.ScheduledNetResponse
	err = json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", resp.Name)
}

func TestGETScheduledNetsFilterByTalkgroupID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "SchedFilter TG",
		Description: "TG for sched filter test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "SchedFilter TG").First(&tg).Error
	require.NoError(t, err)

	body := apimodels.ScheduledNetPost{
		TalkgroupID: tg.ID,
		Name:        "Filtered Sched",
		DayOfWeek:   4,
		TimeOfDay:   "18:00",
		Timezone:    "UTC",
	}
	_, createW := testutils.CreateScheduledNet(t, router, adminJar, body)
	require.Equal(t, http.StatusCreated, createW.Code)

	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, fmt.Sprintf("/api/v1/nets/scheduled?talkgroup_id=%d", tg.ID), nil)
	require.NoError(t, err)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total         int                              `json:"total"`
		ScheduledNets []apimodels.ScheduledNetResponse `json:"scheduled_nets"`
	}
	err = json.Unmarshal(w.Body.Bytes(), &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Len(t, result.ScheduledNets, 1)
	assert.Equal(t, "Filtered Sched", result.ScheduledNets[0].Name)
}

func strPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

func TestPATCHNetShowcaseActiveNet(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create talkgroup and start a net.
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Showcase TG",
		Description: "TG for showcase testing",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Showcase TG").First(&tg).Error
	require.NoError(t, err)

	startResp, startW := testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Showcase test net",
	})
	require.Equal(t, http.StatusCreated, startW.Code)
	assert.False(t, startResp.Showcase)

	// Toggle showcase on.
	patchResp, patchW := testutils.PatchNet(t, router, adminJar, startResp.ID, apimodels.NetPatch{
		Showcase: boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, patchW.Code)
	assert.True(t, patchResp.Showcase)

	// Confirm via GET.
	getResp, getW := testutils.GetNet(t, router, adminJar, startResp.ID)
	assert.Equal(t, http.StatusOK, getW.Code)
	assert.True(t, getResp.Showcase)

	// Toggle showcase off.
	patchResp, patchW = testutils.PatchNet(t, router, adminJar, startResp.ID, apimodels.NetPatch{
		Showcase: boolPtr(false),
	})
	assert.Equal(t, http.StatusOK, patchW.Code)
	assert.False(t, patchResp.Showcase)
}

func TestPATCHNetShowcaseInactiveNet(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create talkgroup, start and stop a net.
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Showcase Ended TG",
		Description: "TG for ended showcase testing",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Showcase Ended TG").First(&tg).Error
	require.NoError(t, err)

	startResp, startW := testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Ended showcase test",
	})
	require.Equal(t, http.StatusCreated, startW.Code)

	_, stopW := testutils.StopNet(t, router, adminJar, startResp.ID)
	require.Equal(t, http.StatusOK, stopW.Code)

	// Toggle showcase on for an inactive net.
	patchResp, patchW := testutils.PatchNet(t, router, adminJar, startResp.ID, apimodels.NetPatch{
		Showcase: boolPtr(true),
	})
	assert.Equal(t, http.StatusOK, patchW.Code)
	assert.True(t, patchResp.Showcase)
	assert.False(t, patchResp.Active)
}

func TestPATCHNetShowcaseUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create talkgroup and start a net.
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Unauth Showcase TG",
		Description: "TG for unauth showcase",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Unauth Showcase TG").First(&tg).Error
	require.NoError(t, err)

	startResp, startW := testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{
		TalkgroupID: tg.ID,
	})
	require.Equal(t, http.StatusCreated, startW.Code)

	// Try PATCH without auth.
	w := httptest.NewRecorder()
	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	patchBody := apimodels.NetPatch{Showcase: boolPtr(true)}
	jsonBytes, err := json.Marshal(patchBody)
	require.NoError(t, err)

	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, fmt.Sprintf("/api/v1/nets/%d", startResp.ID), bytes.NewBuffer(jsonBytes))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestPATCHNetShowcaseNotFound(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	patchResp, patchW := testutils.PatchNet(t, router, adminJar, 999999, apimodels.NetPatch{
		Showcase: boolPtr(true),
	})
	assert.Equal(t, http.StatusNotFound, patchW.Code)
	_ = patchResp
}

func TestGETNetsShowcaseFilter(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create talkgroup.
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Showcase Filter TG",
		Description: "TG for showcase filter test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Showcase Filter TG").First(&tg).Error
	require.NoError(t, err)

	// Start a net and mark as showcase.
	startResp, startW := testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Featured net",
	})
	require.Equal(t, http.StatusCreated, startW.Code)

	_, patchW := testutils.PatchNet(t, router, adminJar, startResp.ID, apimodels.NetPatch{
		Showcase: boolPtr(true),
	})
	require.Equal(t, http.StatusOK, patchW.Code)

	// List with showcase filter.
	w, body := testutils.ListNets(t, router, adminJar, "showcase=true")
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total int                     `json:"total"`
		Nets  []apimodels.NetResponse `json:"nets"`
	}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	require.Len(t, result.Nets, 1)
	assert.True(t, result.Nets[0].Showcase)
	assert.Equal(t, "Featured net", result.Nets[0].Description)
}

func TestGETNetsShowcaseIncludesInactiveNets(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	// Create talkgroup.
	_, tgW := testutils.CreateTalkgroup(t, router, adminJar, apimodels.TalkgroupPost{
		Name:        "Showcase Inactive TG",
		Description: "TG for showcase inactive test",
	})
	require.Equal(t, http.StatusOK, tgW.Code)

	var tg models.Talkgroup
	err = tdb.DB().Where("name = ?", "Showcase Inactive TG").First(&tg).Error
	require.NoError(t, err)

	// Start a net, mark as showcase, then stop.
	startResp, startW := testutils.StartNet(t, router, adminJar, apimodels.NetStartPost{
		TalkgroupID: tg.ID,
		Description: "Inactive showcase net",
	})
	require.Equal(t, http.StatusCreated, startW.Code)

	_, patchW := testutils.PatchNet(t, router, adminJar, startResp.ID, apimodels.NetPatch{
		Showcase: boolPtr(true),
	})
	require.Equal(t, http.StatusOK, patchW.Code)

	_, stopW := testutils.StopNet(t, router, adminJar, startResp.ID)
	require.Equal(t, http.StatusOK, stopW.Code)

	// Showcase filter should still include the stopped net.
	w, body := testutils.ListNets(t, router, adminJar, "showcase=true")
	assert.Equal(t, http.StatusOK, w.Code)

	var result struct {
		Total int                     `json:"total"`
		Nets  []apimodels.NetResponse `json:"nets"`
	}
	err = json.Unmarshal(body, &result)
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	require.Len(t, result.Nets, 1)
	assert.True(t, result.Nets[0].Showcase)
	assert.False(t, result.Nets[0].Active)
}
