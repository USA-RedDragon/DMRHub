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

package sessionstore_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/sessionstore"
	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newStore(t *testing.T) (*sessionstore.Store, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:store_%p?mode=memory&cache=shared", t)))
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&models.Session{}))
	t.Cleanup(func() {
		sqlDB, _ := db.DB()
		_ = sqlDB.Close()
	})
	return sessionstore.New(db, []byte("test-secret")), db
}

func request(t *testing.T, cookies []*http.Cookie) *http.Request {
	t.Helper()
	r := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/", nil)
	for _, c := range cookies {
		r.AddCookie(c)
	}
	return r
}

func TestStoreKeepsASessionAcrossRequests(t *testing.T) {
	t.Parallel()
	st, db := newStore(t)

	s, err := st.New(request(t, nil), "sessions")
	require.NoError(t, err)
	assert.True(t, s.IsNew)
	s.Values["user_id"] = uint(1000001)
	w := httptest.NewRecorder()
	require.NoError(t, st.Save(request(t, nil), w, s))
	cookies := w.Result().Cookies()
	require.Len(t, cookies, 1)

	again, err := st.New(request(t, cookies), "sessions")
	require.NoError(t, err)
	assert.False(t, again.IsNew)
	assert.Equal(t, s.ID, again.ID)
	assert.Equal(t, uint(1000001), again.Values["user_id"])

	again.Options.MaxAge = -1
	require.NoError(t, st.Save(request(t, cookies), httptest.NewRecorder(), again))
	var n int64
	require.NoError(t, db.Model(&models.Session{}).Count(&n).Error)
	assert.Zero(t, n)
	gone, err := st.New(request(t, cookies), "sessions")
	require.NoError(t, err)
	assert.True(t, gone.IsNew)
}

func TestStoreIgnoresAndCleansExpiredSessions(t *testing.T) {
	t.Parallel()
	st, db := newStore(t)
	s, err := st.New(request(t, nil), "sessions")
	require.NoError(t, err)
	w := httptest.NewRecorder()
	require.NoError(t, st.Save(request(t, nil), w, s))
	require.NoError(t, db.Model(&models.Session{}).Where("id = ?", s.ID).Update("expires_at", time.Now().Add(-time.Minute)).Error)

	expired, err := st.New(request(t, w.Result().Cookies()), "sessions")
	require.NoError(t, err)
	assert.True(t, expired.IsNew)

	st.Cleanup(context.Background())
	var n int64
	require.NoError(t, db.Model(&models.Session{}).Count(&n).Error)
	assert.Zero(t, n)
}
