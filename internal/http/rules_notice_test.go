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

package http_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestDeletesAnnounceARulesChange deletes a user, a repeater and a
// talkgroup through the API. Each delete must tell every replica to read
// the routing rules again, so that no replica keeps the rules of what is
// gone, such as a slot lock open to a deleted user.
func TestDeletesAnnounceARulesChange(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithConfig(func(c *config.Config) { c.DMR.DisableRadioIDValidation = true })
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	testutils.SeedAccount(t, tdb.DB(), 3191868, "KI5VMF", "owner")
	testutils.SeedAccount(t, tdb.DB(), 3191869, "N0CALL", "listed")
	_, _, admin := testutils.LoginAdmin(t, router)
	_, w := testutils.CreateRepeater(t, router, testutils.Login(t, router, "owner"), apimodels.RepeaterPost{RadioID: 311860, Type: "mmdvm"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	_, w = testutils.CreateTalkgroup(t, router, admin, apimodels.TalkgroupPost{ID: 50001, Name: "Club", Description: "Club talkgroup"})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())

	sub := tdb.PubSub().Subscribe("hub:rules:changed")
	t.Cleanup(func() { _ = sub.Close() })
	for _, path := range []string{"/api/v1/users/3191869", "/api/v1/repeaters/311860", "/api/v1/talkgroups/50001"} {
		w := testutils.Do(t, router, admin, http.MethodDelete, path, nil)
		require.Equal(t, http.StatusOK, w.Code, w.Body.String())
		id := path[strings.LastIndex(path, "/")+1:]
		select {
		case msg := <-sub.Channel():
			require.True(t, strings.HasSuffix(string(msg), ":"+id), "notice %q for %s", msg, path)
		case <-time.After(2 * time.Second):
			require.FailNow(t, "no rules change notice", "DELETE %s", path)
		}
	}
}
