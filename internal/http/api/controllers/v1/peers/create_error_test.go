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

// TestPOSTPeerWithAnUnknownOwnerFails creates a peer for an owner that does
// not exist. The API must say so and store no peer, not answer that the
// peer was created.
func TestPOSTPeerWithAnUnknownOwnerFails(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	defer tdb.CloseDB()

	_, _, adminJar := testutils.LoginAdmin(t, router)
	resp, w := testutils.CreatePeer(t, router, adminJar, apimodels.PeerPost{
		ID: 200005, OwnerID: 9999999, IP: "10.0.0.5", Port: 62035, Ingress: true, Egress: true,
	})
	assert.NotEqual(t, http.StatusOK, w.Code, "body %s", w.Body.String())
	assert.Empty(t, resp.Password)
	var n int64
	require.NoError(t, tdb.DB().Model(&models.Peer{}).Where("id = ?", 200005).Count(&n).Error)
	assert.Zero(t, n)
}
