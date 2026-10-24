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

package lastheard_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testTimeout = 1 * time.Minute

func TestGETLastheardUnauthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/lastheard", nil)
	router.ServeHTTP(w, req)

	// Lastheard is accessible without login (returns public calls)
	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGETLastheardAuthenticated(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/lastheard", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGETLastheardTalkgroup(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/lastheard/talkgroup/9990", nil)
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestGETLastheardUserInvalidID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/lastheard/user/notanumber", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestGETLastheardRepeaterInvalidID(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	if err != nil {
		t.Fatalf("Failed to create test DB router: %v", err)
	}
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)

	w := httptest.NewRecorder()

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "/api/v1/lastheard/repeater/notanumber", nil)
	for _, cookie := range adminJar.Cookies() {
		req.Header.Add("Cookie", cookie.String())
	}
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func collectKeys(v any, path string, out map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			out[path+"."+k] = true
			collectKeys(child, path+"."+k, out)
		}
	case []any:
		for _, child := range x {
			collectKeys(child, path+"[]", out)
		}
	}
}

func TestLastheardAnswersCallDTOs(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	db := tdb.DB()

	src := models.User{ID: 3191868, Callsign: "KI5VMF", Username: "src", Email: "src@example.com", Approved: true}
	dst := models.User{ID: 3191869, Callsign: "KI5VMG", Username: "dst", Email: "dst@example.com", Approved: true}
	require.NoError(t, db.Create(&src).Error)
	require.NoError(t, db.Create(&dst).Error)
	tg := models.Talkgroup{ID: 91, Name: "TG91", Description: "World"}
	require.NoError(t, db.Create(&tg).Error)
	rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311860, Callsign: "KI5VMF"}, OwnerID: src.ID, Type: models.RepeaterTypeMMDVM}
	require.NoError(t, db.Create(&rpt).Error)
	tgID, dstID, srcID, rptID := tg.ID, dst.ID, src.ID, rpt.ID
	start := time.Now()
	require.NoError(t, db.Create(&models.Call{StreamID: 1, StartTime: start, UserID: &srcID, RepeaterID: &rptID, IngressID: rpt.ID, SourceID: src.ID, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tgID, DestinationID: tg.ID}).Error)
	require.NoError(t, db.Create(&models.Call{StreamID: 2, StartTime: start, UserID: &srcID, RepeaterID: &rptID, IngressID: rpt.ID, SourceID: src.ID, IsToUser: true, ToUserID: &dstID, DestinationID: dst.ID}).Error)

	_, _, adminJar := testutils.LoginAdmin(t, router)
	paths := []string{
		"/api/v1/lastheard",
		"/api/v1/lastheard/user/3191868",
		"/api/v1/lastheard/repeater/311860",
		"/api/v1/lastheard/talkgroup/91",
	}
	allowed := map[string]bool{
		"id": true, "user": true, "repeater": true, "start_time": true, "duration": true, "active": true,
		"time_slot": true, "group_call": true, "repeater_id": true, "is_to_talkgroup": true, "to_talkgroup": true,
		"is_to_user": true, "to_user": true, "is_to_repeater": true, "to_repeater": true, "destination_id": true,
		"loss": true, "jitter": true, "ber": true, "rssi": true,
		"kind": true, "sub_kind": true, "source_id": true, "transport": true, "bursts": true, "fid": true,
		"opcode": true, "sap": true, "data_format": true, "confirmed": true, "blocks": true, "octets": true,
		"bad_crc": true, "lost_blocks": true, "response": true, "talker_alias": true, "alias_format": true,
		"grid": true, "latitude": true, "longitude": true, "position_error": true, "partial": true, "undeliverable": true,
		"codec": true, "codec2_mode": true, "sent_ambe": true, "sent_codec2": true, "missed_ms": true, "busy": true,
		"user.id": true, "user.callsign": true, "repeater.id": true, "repeater.callsign": true,
		"to_user.id": true, "to_user.callsign": true, "to_repeater.id": true, "to_repeater.callsign": true,
		"to_talkgroup.id": true, "to_talkgroup.name": true, "to_talkgroup.description": true,
	}
	for _, path := range paths {
		ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
		for _, cookie := range adminJar.Cookies() {
			req.Header.Add("Cookie", cookie.String())
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		cancel()
		require.Equal(t, http.StatusOK, w.Code, path)

		var body struct {
			Calls []map[string]any `json:"calls"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), path)
		require.NotEmpty(t, body.Calls, path)
		for _, call := range body.Calls {
			keys := map[string]bool{}
			collectKeys(call, "", keys)
			for k := range keys {
				assert.True(t, allowed[k[1:]], "%s returns %s", path, k[1:])
			}
			user, ok := call["user"].(map[string]any)
			require.True(t, ok, path)
			assert.EqualValues(t, src.ID, user["id"], path)
			assert.Equal(t, src.Callsign, user["callsign"], path)
			repeater, ok := call["repeater"].(map[string]any)
			require.True(t, ok, path)
			assert.EqualValues(t, rpt.ID, repeater["id"], path)
		}
	}
}
