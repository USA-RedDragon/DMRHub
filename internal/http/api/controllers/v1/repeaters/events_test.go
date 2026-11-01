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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestTheConnectionLogShowsTheLastDayToSysops reads the connection log of
// a repeater as its owner, a co-sysop and another user. The sysops get the
// entries of the last day, newest first, with their addresses; the other
// user gets nothing.
func TestTheConnectionLogShowsTheLastDayToSysops(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newCosysopEnv(t)
		now := time.Now()
		_, err := models.InviteCosysop(e.db, cosysopRepeater, 3191869, 3191868, now)
		require.NoError(t, err)
		_, err = models.AnswerCosysopInvite(e.db, cosysopRepeater, 3191869, true, now)
		require.NoError(t, err)
		for _, ev := range []models.RepeaterEvent{
			{RepeaterID: cosysopRepeater, Kind: "connected", Address: "203.0.113.7", LastAt: now.Add(-25 * time.Hour)},
			{RepeaterID: cosysopRepeater, Kind: "timeout", Address: "203.0.113.7", Detail: "60", LastAt: now.Add(-2 * time.Hour)},
			{RepeaterID: cosysopRepeater, Kind: "wrong_password", Address: "203.0.113.7", LastAt: now.Add(-time.Hour)},
		} {
			_, err := models.NoteRepeaterEvent(e.db, ev, 1)
			require.NoError(t, err)
		}
		_, err = models.NoteRepeaterEvent(e.db, models.RepeaterEvent{RepeaterID: cosysopRepeater, Kind: "wrong_password", Address: "203.0.113.7", LastAt: now}, 13)
		require.NoError(t, err)

		for _, jar := range []testutils.CookieJar{e.owner, e.kevin} {
			w := testutils.Do(t, e.router, jar, http.MethodGet, "/api/v1/repeaters/319186801/events", nil)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var out struct {
				Events []models.RepeaterEvent `json:"events"`
			}
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &out))
			require.Len(t, out.Events, 2)
			require.Equal(t, "wrong_password", out.Events[0].Kind)
			require.Equal(t, uint(14), out.Events[0].Count)
			require.Equal(t, "203.0.113.7", out.Events[0].Address)
			require.WithinDuration(t, now.Add(-time.Hour), out.Events[0].FirstAt, time.Second)
			require.Equal(t, "timeout", out.Events[1].Kind)
		}
		w := testutils.Do(t, e.router, e.other, http.MethodGet, "/api/v1/repeaters/319186801/events", nil)
		require.Equal(t, http.StatusForbidden, w.Code)
	})
}
