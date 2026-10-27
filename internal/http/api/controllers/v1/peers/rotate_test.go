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

package peers_test

import (
	"encoding/json"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

const rotatePath = "/api/v1/peers/3000/rotate-password"

// changeLog records the peers whose change the hub announced.
type changeLog struct {
	mu  sync.Mutex
	ids []uint
}

func (l *changeLog) add(id uint) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.ids = append(l.ids, id)
}

func (l *changeLog) list() []uint {
	l.mu.Lock()
	defer l.mu.Unlock()
	return append([]uint(nil), l.ids...)
}

// peerPassword returns the stored password of peer 3000.
func peerPassword(t *testing.T, tdb *testutils.TestDB) string {
	t.Helper()
	p, err := models.FindPeerByID(tdb.DB(), 3000)
	require.NoError(t, err)
	return p.Password
}

func seedRotatePeer(t *testing.T, tdb *testutils.TestDB) {
	t.Helper()
	database := tdb.DB()
	testutils.SeedAccount(t, database, 3191868, "KI5VMF", "owner")
	testutils.SeedAccount(t, database, 3191869, "N0CALL", "other")
	require.NoError(t, database.Create(&models.Peer{ID: 3000, OwnerID: 3191868, Password: "oldPassword1!", IP: "192.0.2.10", Port: 62035}).Error)
}

// TestAnOwnerRotatesThePasswordOfTheirPeer rotates the password of a peer
// as its owner: the answer holds only the new password and the time, the
// new password is of the safe character set, the old one is gone from the
// database, and the hub announces the change of the peer to every replica.
func TestAnOwnerRotatesThePasswordOfTheirPeer(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	seedRotatePeer(t, tdb)
	var announced changeLog
	defer tdb.Hub().OnPeerChange(announced.add)()

	before := time.Now()
	w := testutils.Do(t, router, testutils.Login(t, router, "owner"), http.MethodPost, rotatePath, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, map[string]bool{"password": true, "rotated_at": true}, testutils.JSONKeys(t, w.Body.Bytes()))
	var res apimodels.PeerRotatePasswordResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))
	require.Regexp(t, regexp.MustCompile(`^[A-Za-z0-9._-]{13}$`), res.Password)
	require.NotEqual(t, "oldPassword1!", res.Password)
	require.Equal(t, res.Password, peerPassword(t, tdb))
	require.False(t, res.RotatedAt.Before(before.Truncate(time.Second)))
	require.Eventually(t, func() bool { return len(announced.list()) == 1 }, time.Second, 10*time.Millisecond)
	require.Equal(t, []uint{3000}, announced.list())
}

// TestOnlyTheOwnerOrAnAdminRotatesAPeerPassword checks who may rotate the
// password of a peer: a visitor and another user may not, and an admin may.
func TestOnlyTheOwnerOrAnAdminRotatesAPeerPassword(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	seedRotatePeer(t, tdb)
	var announced changeLog
	defer tdb.Hub().OnPeerChange(announced.add)()

	w := testutils.Do(t, router, testutils.CookieJar{}, http.MethodPost, rotatePath, nil)
	require.Equal(t, http.StatusUnauthorized, w.Code, w.Body.String())
	w = testutils.Do(t, router, testutils.Login(t, router, "other"), http.MethodPost, rotatePath, nil)
	require.Equal(t, http.StatusForbidden, w.Code, w.Body.String())
	require.Equal(t, "oldPassword1!", peerPassword(t, tdb))
	require.Empty(t, announced.list())

	_, _, adminJar := testutils.LoginAdmin(t, router)
	w = testutils.Do(t, router, adminJar, http.MethodPost, rotatePath, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.NotEqual(t, "oldPassword1!", peerPassword(t, tdb))

	for _, c := range []struct {
		path string
		code int
	}{
		{"/api/v1/peers/3001/rotate-password", http.StatusNotFound},
		{"/api/v1/peers/abc/rotate-password", http.StatusBadRequest},
	} {
		w := testutils.Do(t, router, adminJar, http.MethodPost, c.path, nil)
		require.Equal(t, c.code, w.Code, "%s: %s", c.path, w.Body.String())
	}
	require.Eventually(t, func() bool { return len(announced.list()) == 1 }, time.Second, 10*time.Millisecond)
}

// TestAStalePeerPatchKeepsTheRotatedPassword saves the peer after its
// password was rotated, with a PATCH of a page that was open before the
// rotation: the new password must stay.
func TestAStalePeerPatchKeepsTheRotatedPassword(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()
	seedRotatePeer(t, tdb)
	jar := testutils.Login(t, router, "owner")

	w := testutils.Do(t, router, jar, http.MethodPost, rotatePath, nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	var res apimodels.PeerRotatePasswordResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &res))

	ip, port, on := "192.0.2.11", 62036, true
	_, w = testutils.PatchPeer(t, router, 3000, jar, apimodels.PeerPatch{IP: &ip, Port: &port, Ingress: &on, Egress: &on})
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	require.Equal(t, res.Password, peerPassword(t, tdb))
}
