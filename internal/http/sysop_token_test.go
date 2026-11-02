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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/oauth"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAScopedTokenCannotUseTheSysopRoutes makes a personal token with every
// scope and calls each route of co-sysops, the connection log, the sysop
// actions and the offline alert with it: no scope covers them, so each
// gets insufficient_scope. A token with full account access reaches them.
func TestAScopedTokenCannotUseTheSysopRoutes(t *testing.T) {
	t.Parallel()
	e := newOAuthEnv(t)
	db := e.tdb.DB()
	admin, found, err := models.FindUserByUsername(db, "Admin")
	require.NoError(t, err)
	require.True(t, found)
	require.NoError(t, db.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 300206801}, Type: models.RepeaterTypeMMDVM, Password: "password1234", OwnerID: admin.ID}).Error)
	scopes := make([]string, 0, len(oauth.Scopes()))
	for _, s := range oauth.Scopes() {
		scopes = append(scopes, s.Name)
	}
	scoped := makeToken(t, e, e.jar, map[string]any{"name": "every scope", "scopes": scopes, "expires_in_days": 30})
	full := makeToken(t, e, e.jar, map[string]any{"name": "full", "full": true, "expires_in_days": 30})

	routes := []struct{ method, path, body string }{
		{http.MethodGet, "/api/v1/repeaters/300206801/cosysops", ""},
		{http.MethodPost, "/api/v1/repeaters/300206801/cosysops", `{"user":"nobody"}`},
		{http.MethodDelete, "/api/v1/repeaters/300206801/cosysops/1", ""},
		{http.MethodGet, "/api/v1/repeaters/cosysop-invites", ""},
		{http.MethodPost, "/api/v1/repeaters/300206801/cosysop-invite/accept", ""},
		{http.MethodPost, "/api/v1/repeaters/300206801/cosysop-invite/decline", ""},
		{http.MethodGet, "/api/v1/repeaters/300206801/events", ""},
		{http.MethodGet, "/api/v1/repeaters/300206801/on-air", ""},
		{http.MethodPost, "/api/v1/repeaters/300206801/drop-call", `{"call_id":1}`},
		{http.MethodPost, "/api/v1/repeaters/300206801/reset", ""},
		{http.MethodGet, "/api/v1/repeaters/300206801/alert", ""},
		{http.MethodPut, "/api/v1/repeaters/300206801/alert", `{"enabled":false}`},
	}
	for _, r := range routes {
		header := http.Header{"Authorization": {"Bearer " + scoped.Secret}, "Origin": {appOrigin}, "Content-Type": {"application/json"}}
		w := e.request(t, r.method, r.path, header, r.body)
		assert.Equal(t, http.StatusForbidden, w.Code, "%s %s: %s", r.method, r.path, w.Body.String())
		assert.Contains(t, w.Body.String(), "insufficient_scope", "%s %s", r.method, r.path)
	}
	for _, path := range []string{"/api/v1/repeaters/300206801/cosysops", "/api/v1/repeaters/300206801/events", "/api/v1/repeaters/300206801/alert"} {
		w := e.bearer(t, http.MethodGet, path, full.Secret)
		assert.Equal(t, http.StatusOK, w.Code, "%s: %s", path, w.Body.String())
	}
}
