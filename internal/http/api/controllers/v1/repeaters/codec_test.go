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

package repeaters_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func codecPatch(ts1, ts2 string) apimodels.RepeaterPatch {
	p := apimodels.RepeaterPatch{}
	if ts1 != "" {
		p.TS1Codec = &ts1
	}
	if ts2 != "" {
		p.TS2Codec = &ts2
	}
	return p
}

// TestPATCHRepeaterCodec sets the codec of each timeslot as the owner and
// as an admin. Another user may not, a value other than auto, codec2 or
// ambe is refused, and the repeater shows the settings and what each slot
// heard.
func TestPATCHRepeaterCodec(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	owner := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
	_, _, ownerJar := testutils.CreateAndLoginUser(t, router, owner)
	other := apimodels.UserRegistration{DMRId: 3191869, Callsign: "N0CALL", Username: "other", Password: "password"}
	_, _, otherJar := testutils.CreateAndLoginUser(t, router, other)
	repeaterPost := apimodels.RepeaterPost{RadioID: 319186811}
	_, w := testutils.CreateRepeater(t, router, ownerJar, repeaterPost)
	require.Equal(t, http.StatusOK, w.Code)

	rpt, w := testutils.GetRepeater(t, router, repeaterPost.RadioID, ownerJar)
	require.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, models.CodecSettingAuto, rpt.TS1Codec, "a new timeslot follows what it hears")
	assert.Equal(t, models.CodecSettingAuto, rpt.TS2Codec)

	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, ownerJar, codecPatch(models.CodecCodec2, ""))
	require.Equal(t, http.StatusOK, w.Code)
	var stored models.Repeater
	require.NoError(t, tdb.DB().First(&stored, repeaterPost.RadioID).Error)
	assert.Equal(t, models.CodecCodec2, stored.TS1Codec)
	assert.Equal(t, models.CodecSettingAuto, stored.TS2Codec, "a PATCH without TS2 keeps it")
	assert.Equal(t, models.CodecCodec2, stored.SlotCodec(false))

	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, otherJar, codecPatch(models.CodecAMBE, ""))
	assert.Equal(t, http.StatusUnauthorized, w.Code, "only the owner or an admin may change the codec")
	stored = models.Repeater{}
	require.NoError(t, tdb.DB().First(&stored, repeaterPost.RadioID).Error)
	assert.Equal(t, models.CodecCodec2, stored.TS1Codec)

	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, ownerJar, codecPatch("amr", ""))
	assert.Equal(t, http.StatusBadRequest, w.Code)

	_, _, adminJar := testutils.LoginAdmin(t, router)
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, adminJar, codecPatch(models.CodecSettingAuto, models.CodecAMBE))
	require.Equal(t, http.StatusOK, w.Code)
	stored = models.Repeater{}
	require.NoError(t, tdb.DB().First(&stored, repeaterPost.RadioID).Error)
	assert.Equal(t, models.CodecSettingAuto, stored.TS1Codec)
	assert.Equal(t, models.CodecAMBE, stored.TS2Codec)

	heard := time.Date(2026, 10, 24, 19, 2, 0, 0, time.UTC)
	require.NoError(t, models.SetRepeaterHeardCodec(tdb.DB(), repeaterPost.RadioID, false, models.CodecCodec2, 3200, heard))
	rpt, w = testutils.GetRepeater(t, router, repeaterPost.RadioID, otherJar)
	require.Equal(t, http.StatusOK, w.Code, "any user may see the codec")
	assert.Equal(t, models.CodecCodec2, rpt.TS1HeardCodec)
	assert.Equal(t, 3200, rpt.TS1HeardCodec2Mode)
	require.NotNil(t, rpt.TS1HeardAt)
	assert.True(t, heard.Equal(*rpt.TS1HeardAt))
	assert.Empty(t, rpt.TS2HeardCodec)
	assert.Nil(t, rpt.TS2HeardAt)
}

// TestStaleRepeaterSaveKeepsCodec saves a copy of a repeater from before a
// change of its codec, as the repeater servers do at a login. The change
// must stay.
func TestStaleRepeaterSaveKeepsCodec(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
	repeaterPost := apimodels.RepeaterPost{RadioID: 319186812}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	require.Equal(t, http.StatusOK, w.Code)

	stale, err := models.FindRepeaterByID(tdb.DB(), repeaterPost.RadioID)
	require.NoError(t, err)
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, codecPatch(models.CodecCodec2, ""))
	require.Equal(t, http.StatusOK, w.Code)
	require.NoError(t, models.SetRepeaterHeardCodec(tdb.DB(), repeaterPost.RadioID, true, models.CodecCodec2, 2400, time.Now()))

	stale.SimplexRepeater = true
	require.NoError(t, tdb.DB().Save(&stale).Error)
	fresh, err := models.FindRepeaterByID(tdb.DB(), repeaterPost.RadioID)
	require.NoError(t, err)
	assert.True(t, fresh.SimplexRepeater)
	assert.Equal(t, models.CodecCodec2, fresh.TS1Codec, "a save of a stale repeater must not undo the codec setting")
	assert.Equal(t, models.CodecCodec2, fresh.TS2HeardCodec, "a save of a stale repeater must not undo the codec heard")
	assert.Equal(t, 2400, fresh.TS2HeardCodec2Mode)
}

// TestPATCHRepeaterCodecIPSCIsAMBE refuses Codec2 on an IPSC repeater,
// which carries only AMBE.
func TestPATCHRepeaterCodecIPSCIsAMBE(t *testing.T) {
	t.Parallel()

	router, tdb, err := testutils.CreateTestDBRouterWithHub()
	require.NoError(t, err)
	defer tdb.CloseDB()

	user := apimodels.UserRegistration{DMRId: 3191868, Callsign: "KI5VMF", Username: "testuser", Password: "password"}
	_, _, userJar := testutils.CreateAndLoginUser(t, router, user)
	repeaterPost := apimodels.RepeaterPost{RadioID: 319186813, Type: models.RepeaterTypeIPSC}
	_, w := testutils.CreateRepeater(t, router, userJar, repeaterPost)
	require.Equal(t, http.StatusOK, w.Code)

	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, codecPatch(models.CodecCodec2, ""))
	assert.Equal(t, http.StatusBadRequest, w.Code)
	_, w = testutils.PatchRepeater(t, router, repeaterPost.RadioID, userJar, codecPatch(models.CodecAMBE, ""))
	assert.Equal(t, http.StatusOK, w.Code)
}
