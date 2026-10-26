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
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type undeliverableEntry struct {
	Sentence   string `json:"sentence"`
	Target     string `json:"target"`
	RepeaterID uint   `json:"repeater_id"`
	Callsign   string `json:"callsign"`
	Reason     string `json:"reason"`
}

type undeliverableRow struct {
	Undeliverable []json.RawMessage `json:"undeliverable"`
	Scope         string            `json:"undeliverable_scope"`
}

func allUndeliverable() []undeliverableEntry {
	return []undeliverableEntry{
		{Sentence: "IPSC repeater 314926 was busy on that slot.", Target: "ipsc", RepeaterID: 314926, Callsign: "W1XPR", Reason: "slot_busy"},
		{Sentence: "IPSC repeater 311304200 was busy on that slot.", Target: "ipsc", RepeaterID: 311304200, Callsign: "K5ABC", Reason: "slot_busy"},
		{Sentence: "IPSC repeater 311305510 cannot carry this kind of burst.", Target: "ipsc", RepeaterID: 311305510, Reason: "no_ipsc_form"},
	}
}

// TestLastheardShowsEachViewerOnlyTheirUndeliverable sends a group call
// that three repeaters did not get to five viewers on every last heard
// path. The caller and an admin see every repeater, the owner of one of
// them sees only theirs, and anyone else sees none.
func TestLastheardShowsEachViewerOnlyTheirUndeliverable(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	db := tdb.DB()

	caller := testutils.SeedAccount(t, db, 3191868, "KI5VMF", "caller")
	owner := testutils.SeedAccount(t, db, 3140001, "W1OWN", "owner")
	other := testutils.SeedAccount(t, db, 3150001, "N0OTH", "other")
	testutils.SeedAccount(t, db, other.ID+1, "K5OWN", "k5own")
	tg := models.Talkgroup{ID: 91, Name: "TG91"}
	require.NoError(t, db.Create(&tg).Error)
	for _, r := range []models.Repeater{
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311860, Callsign: "KI5VMF"}, OwnerID: caller.ID, Type: models.RepeaterTypeMMDVM},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 314926, Callsign: "W1XPR"}, OwnerID: owner.ID, Type: models.RepeaterTypeIPSC},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311304200, Callsign: "K5ABC"}, OwnerID: other.ID + 1, Type: models.RepeaterTypeIPSC},
		{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311304201, Callsign: "N0OTH"}, OwnerID: other.ID, Type: models.RepeaterTypeIPSC},
	} {
		require.NoError(t, db.Create(&r).Error)
	}
	tgID, srcID, rptID := tg.ID, caller.ID, uint(311860)
	all := allUndeliverable()
	sentences := make([]string, 0, len(all))
	for _, e := range all {
		sentences = append(sentences, e.Sentence)
	}
	require.NoError(t, db.Create(&models.Call{
		StreamID: 1, StartTime: time.Now(), UserID: &srcID, RepeaterID: &rptID, IngressID: rptID, SourceID: caller.ID,
		GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tgID, DestinationID: tg.ID, Undeliverable: sentences,
	}).Error)

	_, _, adminJar := testutils.LoginAdmin(t, router)
	viewers := []struct {
		name  string
		jar   testutils.CookieJar
		want  []undeliverableEntry
		scope string
	}{
		{"signed out", testutils.CookieJar{}, nil, ""},
		{"another user", testutils.Login(t, router, "other"), nil, ""},
		{"the repeater owner", testutils.Login(t, router, "owner"), all[:1], "own"},
		{"the caller", testutils.Login(t, router, "caller"), all, "all"},
		{"an admin", adminJar, all, "all"},
	}
	paths := []string{
		"/api/v1/lastheard",
		"/api/v1/lastheard/user/3191868",
		"/api/v1/lastheard/repeater/311860",
		"/api/v1/lastheard/talkgroup/91",
	}
	for _, v := range viewers {
		for _, path := range paths {
			w := testutils.Do(t, router, v.jar, http.MethodGet, path, nil)
			restricted := path == "/api/v1/lastheard/user/3191868" || path == "/api/v1/lastheard/repeater/311860"
			if restricted && v.scope != "all" && (w.Code == http.StatusUnauthorized || w.Code == http.StatusForbidden) {
				continue
			}
			require.Equal(t, http.StatusOK, w.Code, "%s %s", v.name, path)
			var body struct {
				Calls []undeliverableRow `json:"calls"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body), "%s %s", v.name, path)
			require.Len(t, body.Calls, 1, "%s %s", v.name, path)
			assertUndeliverable(t, body.Calls[0], v.want, v.scope, v.name+" "+path)
		}
	}
}

func assertUndeliverable(t *testing.T, row undeliverableRow, want []undeliverableEntry, scope, what string) {
	t.Helper()
	if !assert.Len(t, row.Undeliverable, len(want), "%s sees %d undeliverable entries", what, len(row.Undeliverable)) {
		return
	}
	assert.Equal(t, scope, row.Scope, what)
	if len(want) == 0 {
		return
	}
	got := make([]undeliverableEntry, 0, len(row.Undeliverable))
	for _, raw := range row.Undeliverable {
		var e undeliverableEntry
		if !assert.NoError(t, json.Unmarshal(raw, &e), "%s: entry %s", what, raw) {
			return
		}
		got = append(got, e)
	}
	assert.Equal(t, want, got, what)
}
