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

package users_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheHubLearnsOfAnAddedAndARemovedRadioID lets only members transmit,
// has a radio transmit before its ID is on a list, then adds and removes
// the ID through the API. The hub must take the radio in right after the
// add, and refuse it right after the removal, without waiting for its
// cache of radio owners to expire.
func TestTheHubLearnsOfAnAddedAndARemovedRadioID(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		c.DMR.DisableRadioIDValidation = true
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	h := tdb.Hub()
	t.Cleanup(h.HoldLookupWaits())
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.Routing = config.Routing{Transmit: config.TransmitMembers}
	h.SetConfig(config.NewCurrent(&cfg))
	testutils.SeedAccount(t, tdb.DB(), ownerAccountID, ownerCallsign, "owner")
	jar := testutils.Login(t, router, "owner")

	const radio = uint(3000002)
	admitted := func(stream uint) bool {
		ok := make(chan bool, 1)
		h.RouteStream(context.Background(), models.Packet{
			Signature: string(dmrconst.CommandDMRD), Src: radio, Dst: 9, Repeater: 999998, GroupCall: true,
			FrameType: dmrconst.FrameVoice, DTypeOrVSeq: dmrconst.VoiceA, StreamID: stream, BER: -1, RSSI: -1,
		}, models.RepeaterTypeMMDVM, func(a bool) { ok <- a })
		return <-ok
	}
	require.False(t, admitted(1), "a radio ID on no list may not transmit")

	w := testutils.Do(t, router, jar, http.MethodPost, "/api/v1/users/me/radio-ids", apimodels.RadioIDPost{ID: radio})
	require.Equal(t, http.StatusCreated, w.Code, w.Body.String())
	assert.True(t, admitted(2), "the hub did not learn of the added radio ID")

	w = testutils.Do(t, router, jar, http.MethodDelete, "/api/v1/users/me/radio-ids/3000002", nil)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.False(t, admitted(3), "the hub did not learn of the removed radio ID")
}
