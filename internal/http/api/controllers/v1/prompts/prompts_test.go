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

package prompts_test

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	v1Prompts "github.com/USA-RedDragon/DMRHub/internal/http/api/controllers/v1/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPromptsAreListedAndPreviewedForUsersAndAdmins(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		c.DMR.Routing.PromptVoice = "kitten-mini-bruno"
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()

	assert.Equal(t, http.StatusUnauthorized, testutils.Do(t, router, testutils.CookieJar{}, http.MethodGet, "/api/v1/prompts", nil).Code)

	testutils.SeedAccount(t, db, 3000001, "N0CALL", "user")
	jar := testutils.Login(t, router, "user")
	w := testutils.Do(t, router, jar, http.MethodGet, "/api/v1/prompts", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var list v1Prompts.List
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &list))
	assert.Equal(t, prompts.Voices(), list.Voices)
	assert.Equal(t, "kitten-mini-bruno", list.Default)
	require.Len(t, list.Clips, len(prompts.Clips()))
	assert.Equal(t, v1Prompts.Clip{Name: prompts.TalkgroupNotFound, Text: "Talkgroup not found."}, list.Clips[0])

	w = testutils.Do(t, router, jar, http.MethodGet, "/api/v1/prompts/kitten-mini-luna/unlocked.wav", nil)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "audio/wav", w.Header().Get("Content-Type"))
	want, err := prompts.Preview("kitten-mini-luna", prompts.Unlocked)
	require.NoError(t, err)
	assert.Equal(t, want, w.Body.Bytes())
	for _, path := range []string{"/api/v1/prompts/kitten-mini-luna/unlocked", "/api/v1/prompts/nobody/unlocked.wav", "/api/v1/prompts/kitten-mini-luna/nothing.wav"} {
		assert.Equal(t, http.StatusNotFound, testutils.Do(t, router, jar, http.MethodGet, path, nil).Code, path)
	}

	assert.Equal(t, http.StatusForbidden, testutils.Do(t, router, jar, http.MethodGet, "/api/v1/config/prompts/unlocked.wav", nil).Code, "the Setup preview is for admins")
	testutils.SeedAccount(t, db, 3000002, "N0ADMN", "sysop")
	require.NoError(t, db.Model(&models.User{}).Where("id = ?", 3000002).Update("admin", true).Error)
	admin := testutils.Login(t, router, "sysop")
	w = testutils.Do(t, router, admin, http.MethodGet, "/api/v1/config/prompts/unlocked.wav", nil)
	require.Equal(t, http.StatusOK, w.Code)
	want, err = prompts.Preview("kitten-mini-bruno", prompts.Unlocked)
	require.NoError(t, err)
	assert.Equal(t, want, w.Body.Bytes(), "the default voice of the hub")
	w = testutils.Do(t, router, admin, http.MethodGet, "/api/v1/config/prompts/unlocked.wav?voice=kokoro-bm-lewis", nil)
	require.Equal(t, http.StatusOK, w.Code)
	want, err = prompts.Preview("kokoro-bm-lewis", prompts.Unlocked)
	require.NoError(t, err)
	assert.Equal(t, want, w.Body.Bytes())
}
