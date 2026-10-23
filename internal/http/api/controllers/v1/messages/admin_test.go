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
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (e *env) admin(jar testutils.CookieJar, query string) apimodels.AdminMessageList {
	e.t.Helper()
	w := testutils.Do(e.t, e.router, jar, http.MethodGet, "/api/v1/admin/messages"+query, nil)
	require.Equal(e.t, http.StatusOK, w.Code, w.Body.String())
	var l apimodels.AdminMessageList
	require.NoError(e.t, json.Unmarshal(w.Body.Bytes(), &l))
	return l
}

func TestAdminsSeeEveryMessage(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	admin := testutils.Login(t, e.router, "Admin")
	old := time.Now().Add(-48 * time.Hour)
	rows := make([]models.Message, 0, 301)
	for i := range 300 {
		text := fmt.Sprintf("bulk %03d", i)
		rows = append(rows, models.Message{ReceivedAt: old.Add(time.Duration(i) * time.Second), SourceID: other, DestinationID: owner, Format: models.MessageFormatIP, Text: &text})
	}
	require.NoError(t, e.db.CreateInBatches(rows, 100).Error)
	raw := []byte{0x42, 0x45, 0x00, 0x01}
	special := models.Message{ReceivedAt: time.Now(), SourceID: owner, DestinationID: tg, IsGroup: true, Format: models.MessageFormatTMS, RepeaterID: 100001, Slot: 2, Transport: "mmdvm", StreamID: 9, Octets: 4, Blocks: 1, Raw: raw}
	txt := "50% OFF_today"
	special.Text = &txt
	require.NoError(t, e.db.Create(&special).Error)

	l := e.admin(admin, "?limit=100")
	assert.Equal(t, int64(301), l.Total)
	assert.Len(t, l.Messages, 100)
	assert.Equal(t, special.ID, l.Messages[0].ID)
	assert.Nil(t, l.Messages[0].Raw, "a list has no raw octets")
	assert.Equal(t, uint(100001), l.Messages[0].RepeaterID)
	assert.Equal(t, uint8(2), l.Messages[0].Slot)
	assert.Equal(t, "Texas", l.Messages[0].TalkgroupName)
	assert.Len(t, e.admin(admin, "?limit=100&page=4").Messages, 1)
	assert.Empty(t, e.admin(admin, "?limit=100&page=5").Messages)
	assert.Empty(t, e.admin(admin, "?page=9223372036854775807").Messages)

	assert.Equal(t, int64(1), e.admin(admin, fmt.Sprintf("?src=%d", owner)).Total)
	assert.Equal(t, int64(300), e.admin(admin, fmt.Sprintf("?dst=%d", owner)).Total)
	assert.Equal(t, int64(1), e.admin(admin, "?format=tms").Total)
	assert.Equal(t, int64(1), e.admin(admin, "?text="+url.QueryEscape("50% off")).Total)
	assert.Equal(t, int64(0), e.admin(admin, "?text="+url.QueryEscape("5_%")).Total)
	assert.Equal(t, int64(1), e.admin(admin, "?text="+url.QueryEscape("f_t")).Total)
	assert.Equal(t, int64(300), e.admin(admin, "?text=BULK").Total)
	from := url.QueryEscape(old.Add(100 * time.Second).UTC().Format(time.RFC3339))
	to := url.QueryEscape(old.Add(110 * time.Second).UTC().Format(time.RFC3339))
	assert.Equal(t, int64(10), e.admin(admin, "?from="+from+"&to="+to).Total)

	w := testutils.Do(t, e.router, admin, http.MethodGet, fmt.Sprintf("/api/v1/admin/messages/%d", special.ID), nil)
	require.Equal(t, http.StatusOK, w.Code)
	var one apimodels.AdminMessage
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &one))
	require.NotNil(t, one.Raw)
	assert.Equal(t, hex.EncodeToString(raw), *one.Raw)
	w = testutils.Do(t, e.router, admin, http.MethodGet, "/api/v1/admin/messages/999999", nil)
	assert.Equal(t, http.StatusNotFound, w.Code)

	for _, q := range []string{"?src=x", "?dst=-1", "?format=pigeon", "?from=yesterday", "?to=2026-10-22"} {
		w := testutils.Do(t, e.router, admin, http.MethodGet, "/api/v1/admin/messages"+q, nil)
		assert.Equal(t, http.StatusBadRequest, w.Code, q)
	}
}

func TestUsersCannotUseTheAdminSearch(t *testing.T) {
	t.Parallel()
	e := newEnv(t)
	m := e.msg(time.Now(), other, owner, false, "secret")
	for _, jar := range []testutils.CookieJar{e.jar, {}} {
		for _, p := range []string{"/api/v1/admin/messages", fmt.Sprintf("/api/v1/admin/messages/%d", m.ID)} {
			w := testutils.Do(t, e.router, jar, http.MethodGet, p, nil)
			assert.Equal(t, http.StatusUnauthorized, w.Code, p)
			assert.NotContains(t, w.Body.String(), "secret", p)
		}
	}
}
