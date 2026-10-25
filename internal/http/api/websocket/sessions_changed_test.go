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

package websocket_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
)

func TestLogoutClosesOnlyTheSocketsOfThatSession(t *testing.T) {
	t.Parallel()
	for _, be := range userBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newUserEnv(t, be)
			id, jar := e.user(t, false)
			other := e.login(t, id)
			here := open(t, e.primary, "/ws/calls", jar)
			there := open(t, e.replica, "/ws/messages", jar)
			kept := open(t, e.replica, "/ws/calls", other)

			assert.Equal(t, http.StatusOK, e.request(t, jar, http.MethodPost, "/api/v1/auth/logout", nil))
			e.requireClosedFor(t, here, id, "logged out", "the socket of the session on the same replica")
			e.requireClosedFor(t, there, id, "logged out", "the socket of the session on the other replica")
			requireOpen(t, kept, "the socket of another session of the user")
			assert.Equal(t, http.StatusOK, e.request(t, other, http.MethodGet, "/api/v1/users/me", nil))
		})
	}
}

func TestPasswordChangeEndsTheOtherSessionsOfTheUser(t *testing.T) {
	t.Parallel()
	for _, be := range userBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e := newUserEnv(t, be)
			id, jar := e.user(t, false)
			second, third := e.login(t, id), e.login(t, id)
			mine := open(t, e.primary, "/ws/calls", jar)
			mineThere := open(t, e.replica, "/ws/calls", jar)
			theirs := open(t, e.replica, "/ws/calls", second)
			theirsHere := open(t, e.primary, "/ws/messages", third)
			bystanderID, bystanderJar := e.user(t, false)
			bystander := open(t, e.replica, "/ws/calls", bystanderJar)

			path := fmt.Sprintf("/api/v1/users/%d", id)
			assert.Equal(t, http.StatusOK, e.request(t, jar, http.MethodPatch, path, map[string]string{"password": "a-new-password", "current_password": testutils.TestPassword}))
			e.requireClosedFor(t, theirs, id, "password changed", "another session on the other replica")
			e.requireClosedFor(t, theirsHere, id, "password changed", "another session on the same replica")
			requireOpen(t, mine, "the session that changed the password")
			requireOpen(t, mineThere, "the session that changed the password, on the other replica")
			requireOpen(t, bystander, fmt.Sprintf("user %d", bystanderID))
			assert.Equal(t, http.StatusOK, e.request(t, jar, http.MethodGet, "/api/v1/users/me", nil))
			assert.Equal(t, http.StatusUnauthorized, e.request(t, second, http.MethodGet, "/api/v1/users/me", nil))
			assert.Equal(t, http.StatusUnauthorized, e.request(t, third, http.MethodGet, "/api/v1/users/me", nil))
			again, resp := testutils.DialWS(t, e.replica.URL, "/ws/calls", second)
			assert.Nil(t, again, "an ended session opened a socket")
			assert.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestAdminPasswordResetEndsEverySessionOfTheUser(t *testing.T) {
	t.Parallel()
	e := newUserEnv(t, testutils.SQLiteMemoryBackend())
	id, jar := e.user(t, false)
	second := e.login(t, id)
	first := open(t, e.primary, "/ws/calls", jar)
	other := open(t, e.replica, "/ws/calls", second)

	path := fmt.Sprintf("/api/v1/users/%d", id)
	assert.Equal(t, http.StatusOK, e.request(t, e.admin, http.MethodPatch, path, map[string]string{"password": "a-new-password"}))
	e.requireClosedFor(t, first, id, "password changed", "the first session")
	e.requireClosedFor(t, other, id, "password changed", "the second session")
	assert.Equal(t, http.StatusUnauthorized, e.request(t, jar, http.MethodGet, "/api/v1/users/me", nil))
	assert.Equal(t, http.StatusUnauthorized, e.request(t, second, http.MethodGet, "/api/v1/users/me", nil))
	assert.Equal(t, http.StatusOK, e.request(t, e.admin, http.MethodGet, "/api/v1/users/me", nil))
}
