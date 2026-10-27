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

package messages_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdminMessageReadsAreLogged(t *testing.T) {
	t.Parallel()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) { be.Migrated().Setup(t, c) })
			require.NoError(t, err)
			defer tdb.CloseDB()
			db := tdb.DB()
			testutils.SeedAccount(t, db, owner, "KI5VMF", "owner")
			testutils.SeedAccount(t, db, other, "KD5SMP", "other")
			require.NoError(t, db.Create(&models.UserRadioID{UserID: owner, RadioID: addedID, VerifiedAt: time.Now()}).Error)
			text := "hello"
			a := models.Message{ReceivedAt: time.Now().Add(-time.Minute), SourceID: other, DestinationID: owner, Format: models.MessageFormatIP, Text: &text}
			b := models.Message{ReceivedAt: time.Now(), SourceID: addedID, DestinationID: other, Format: models.MessageFormatIP, Text: &text}
			require.NoError(t, db.Create(&a).Error)
			require.NoError(t, db.Create(&b).Error)
			admin := testutils.Login(t, router, "Admin")
			user := testutils.Login(t, router, "owner")

			accesses := func(query string) apimodels.MessageAccessList {
				t.Helper()
				w := testutils.Do(t, router, admin, http.MethodGet, "/api/v1/admin/message-access"+query, nil)
				require.Equal(t, http.StatusOK, w.Code, w.Body.String())
				var l apimodels.MessageAccessList
				require.NoError(t, json.Unmarshal(w.Body.Bytes(), &l))
				return l
			}
			assert.Equal(t, int64(0), accesses("").Total)
			assert.Equal(t, http.StatusForbidden, testutils.Do(t, router, user, http.MethodGet, "/api/v1/admin/message-access", nil).Code)

			before := time.Now().Add(-time.Second)
			require.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, fmt.Sprintf("/api/v1/admin/messages?dst=%d", owner), nil).Code)
			require.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, fmt.Sprintf("/api/v1/admin/messages/%d", b.ID), nil).Code)
			require.Equal(t, http.StatusOK, testutils.Do(t, router, admin, http.MethodGet, "/api/v1/admin/messages?text=nothing", nil).Code)

			l := accesses("")
			require.Equal(t, int64(2), l.Total, "a search that shows no message is not logged")
			read, search := l.Accesses[0], l.Accesses[1]
			assert.Equal(t, models.MessageAccessRead, read.Kind)
			require.NotNil(t, read.MessageID)
			assert.Equal(t, b.ID, *read.MessageID)
			assert.Equal(t, "XXXXXX", read.AdminCallsign)
			assert.True(t, read.At.After(before))
			require.Len(t, read.RadioIDs, 2)
			assert.Equal(t, addedID, read.RadioIDs[1].RadioID)
			require.NotNil(t, read.RadioIDs[1].UserID)
			assert.Equal(t, owner, *read.RadioIDs[1].UserID, "an added radio ID shows its user")
			assert.Equal(t, "KI5VMF", read.RadioIDs[1].Callsign)
			assert.Equal(t, other, read.RadioIDs[0].RadioID)
			assert.Equal(t, "KD5SMP", read.RadioIDs[0].Callsign)

			assert.Equal(t, models.MessageAccessSearch, search.Kind)
			assert.Nil(t, search.MessageID)
			assert.Equal(t, fmt.Sprintf("dst=%d", owner), search.Filter)
			assert.Equal(t, 1, search.Results)

			assert.Equal(t, int64(1), accesses(fmt.Sprintf("?radio_id=%d", addedID)).Total)
			assert.Equal(t, int64(2), accesses(fmt.Sprintf("?radio_id=%d", other)).Total)
			assert.Equal(t, int64(0), accesses(fmt.Sprintf("?admin=%d", owner)).Total)
			assert.Len(t, accesses("?limit=1&page=2").Accesses, 1)
			assert.Equal(t, http.StatusBadRequest, testutils.Do(t, router, admin, http.MethodGet, "/api/v1/admin/message-access?radio_id=x", nil).Code)
		})
	}
}
