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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/require"
)

// TestSysopsSeeTheCallsOnTheAirAndDropOne seeds the active calls of a
// network: one from the repeater on TS2, one to a talkgroup that the
// repeater has on TS1, one to a talkgroup it does not have, and an ended
// one. The repeater shows the first two, a co-sysop drops one, and a call
// that the repeater does not carry cannot be dropped.
func TestSysopsSeeTheCallsOnTheAirAndDropOne(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)
		now := time.Now()
		_, err := models.InviteCosysop(e.db, cosysopRepeater, 3191869, 3191868, now)
		require.NoError(t, err)
		_, err = models.AnswerCosysopInvite(e.db, cosysopRepeater, 3191869, true, now)
		require.NoError(t, err)
		for _, tg := range []uint{3100, 91} {
			require.NoError(t, e.db.Create(&models.Talkgroup{ID: tg, Name: "TG"}).Error)
		}
		require.NoError(t, models.SetRepeaterStaticTalkgroups(e.db, cosysopRepeater, []models.Talkgroup{{ID: 3100}}, nil))
		rpt, kevin := uint(cosysopRepeater), uint(3191869)
		tg3100, tg91 := uint(3100), uint(91)
		calls := []models.Call{
			{StreamID: 11, StartTime: now, Active: true, UserID: &kevin, SourceID: kevin, RepeaterID: &rpt, TimeSlot: true, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tg91, DestinationID: 91, Kind: models.CallKindVoice},
			{StreamID: 12, StartTime: now, Active: true, SourceID: 3191870, TimeSlot: true, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tg3100, DestinationID: 3100, Kind: models.CallKindVoice},
			{StreamID: 13, StartTime: now, Active: true, SourceID: 3191870, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tg91, DestinationID: 91, Kind: models.CallKindVoice},
			{StreamID: 14, StartTime: now.Add(-time.Minute), Active: false, SourceID: 3191870, RepeaterID: &rpt, GroupCall: true, IsToTalkgroup: true, ToTalkgroupID: &tg3100, DestinationID: 3100, Kind: models.CallKindVoice},
		}
		for i := range calls {
			require.NoError(t, e.db.Create(&calls[i]).Error)
		}

		w := testutils.Do(t, e.router, e.kevin, http.MethodGet, "/api/v1/repeaters/319186801/on-air", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		var out struct {
			Calls []apimodels.OnAirCall `json:"calls"`
		}
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
		require.Len(t, out.Calls, 2)
		byID := map[uint]apimodels.OnAirCall{}
		for _, c := range out.Calls {
			byID[c.CallID] = c
		}
		require.Equal(t, apimodels.OnAirCall{CallID: calls[0].ID, Slot: 2, Direction: "from", SourceID: kevin, Callsign: "KI5VMF", Destination: 91, GroupCall: true, StartTime: byID[calls[0].ID].StartTime}, byID[calls[0].ID])
		require.Equal(t, 1, byID[calls[1].ID].Slot)
		require.Equal(t, "to", byID[calls[1].ID].Direction)

		w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/drop-call", apimodels.DropCallPost{CallID: calls[1].ID})
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		for _, id := range []uint{calls[2].ID, calls[3].ID, 999999} {
			w = testutils.Do(t, e.router, e.kevin, http.MethodPost, "/api/v1/repeaters/319186801/drop-call", apimodels.DropCallPost{CallID: id})
			require.Equal(t, http.StatusNotFound, w.Code, "call %d", id)
		}
		w = testutils.Do(t, e.router, e.other, http.MethodGet, "/api/v1/repeaters/319186801/on-air", nil)
		require.Equal(t, http.StatusForbidden, w.Code)
		w = testutils.Do(t, e.router, e.other, http.MethodPost, "/api/v1/repeaters/319186801/drop-call", apimodels.DropCallPost{CallID: calls[1].ID})
		require.Equal(t, http.StatusForbidden, w.Code)
	})
}

// TestASysopResetsTheConnection resets a repeater as a co-sysop: the hub
// tells every replica, and the connection log names the sysop. Another
// user may not.
func TestASysopResetsTheConnection(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		router, tdb, err := testutils.CreateTestDBRouter()
		require.NoError(t, err)
		defer tdb.CloseDB()
		database := tdb.DB()
		owner := testutils.SeedAccount(t, database, 3191868, "KP4DJT", "kp4djt")
		testutils.SeedAccount(t, database, 3191869, "KI5VMF", "kevin")
		testutils.SeedAccount(t, database, 3191870, "N0CALL", "other")
		require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: cosysopRepeater}, Type: models.RepeaterTypeMMDVM, Password: "password1234", OwnerID: owner.ID}).Error)
		now := time.Now()
		_, err = models.InviteCosysop(database, cosysopRepeater, 3191869, owner.ID, now)
		require.NoError(t, err)
		_, err = models.AnswerCosysopInvite(database, cosysopRepeater, 3191869, true, now)
		require.NoError(t, err)
		var resets secretLog
		defer tdb.Hub().OnRepeaterReset(resets.add)()

		w := testutils.Do(t, router, testutils.Login(t, router, "other"), http.MethodPost, "/api/v1/repeaters/319186801/reset", nil)
		require.Equal(t, http.StatusForbidden, w.Code)
		w = testutils.Do(t, router, testutils.Login(t, router, "kevin"), http.MethodPost, "/api/v1/repeaters/319186801/reset", nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		bubble.SettleDB(t, database, time.Minute)
		require.Equal(t, []uint{cosysopRepeater}, resets.list())
		events, err := models.ListRepeaterEvents(database, cosysopRepeater, time.Time{})
		require.NoError(t, err)
		require.Len(t, events, 1)
		require.Equal(t, "reset", events[0].Kind)
		require.Equal(t, "KI5VMF", events[0].Detail)
	})
}
