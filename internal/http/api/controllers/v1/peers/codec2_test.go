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
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPeerCodec2IsForAdmins creates a peer that carries Codec2 and checks
// that only an admin may change that. A new peer is AMBE only.
func TestPeerCodec2IsForAdmins(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)
	testutils.SeedAccount(t, tdb.DB(), 3191868, "KI5VMF", "owner")
	ownerJar := testutils.Login(t, router, "owner")

	_, w := testutils.CreatePeer(t, router, adminJar, apimodels.PeerPost{
		ID: 200003, OwnerID: 3191868, IP: "10.0.0.3", Port: 62035, Ingress: true, Egress: true,
	})
	require.Equal(t, http.StatusOK, w.Code)
	var stored models.Peer
	require.NoError(t, tdb.DB().First(&stored, 200003).Error)
	assert.False(t, stored.Codec2, "a new peer is AMBE only")

	_, w = testutils.CreatePeer(t, router, adminJar, apimodels.PeerPost{
		ID: 200004, OwnerID: 3191868, IP: "10.0.0.4", Port: 62035, Ingress: true, Egress: true, Codec2: true,
	})
	require.Equal(t, http.StatusOK, w.Code)
	stored = models.Peer{}
	require.NoError(t, tdb.DB().First(&stored, 200004).Error)
	assert.True(t, stored.Codec2, "an admin creates a peer that carries Codec2")

	off := false
	_, w = testutils.PatchPeer(t, router, 200004, ownerJar, apimodels.PeerPatch{Codec2: &off})
	assert.Equal(t, http.StatusForbidden, w.Code, "the owner cannot change the codec")
	stored = models.Peer{}
	require.NoError(t, tdb.DB().First(&stored, 200004).Error)
	assert.True(t, stored.Codec2)

	resp, w := testutils.PatchPeer(t, router, 200004, adminJar, apimodels.PeerPatch{Codec2: &off})
	assert.Equal(t, http.StatusOK, w.Code)
	assert.False(t, resp.Codec2)
}
