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

package models_test

import (
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dmrGatewayRPTC builds an RPTC as DMRGateway writes it: CDMRGateway's
// config message with its [Info] latitude and longitude, and for a
// network with Location=0 the coordinates overwritten in writeConfig.
func dmrGatewayRPTC(id uint32, lat, lon float64, location bool) []byte {
	body := fmt.Sprintf("%-8.8s%09d%09d%02d%02d%8.8s%9.9s%03d%-20.20s%-19.19s%c%-124.124s%-40.40s%-40.40s",
		"N0CALL", 438800000, 431200000, 1, 1, fmt.Sprintf("%08f", lat), fmt.Sprintf("%09f", lon), 0,
		"Zilker Park", "Multi-Mode Repeater", '3', "", "20260101_DMRGateway", "MMDVM")
	p := make([]byte, 8, 8+len(body))
	copy(p, "RPTC")
	binary.BigEndian.PutUint32(p[4:], id)
	p = append(p, body...)
	if !location {
		copy(p[38:], "0.00000000.000000")
	}
	return p
}

func TestDMRGatewayLocationSetsTheRPTCPosition(t *testing.T) {
	t.Parallel()
	var on models.RepeaterConfiguration
	require.NoError(t, on.ParseConfig(dmrGatewayRPTC(310999901, 30.2669, -97.7729, true), "v", "c"))
	assert.InDelta(t, 30.2669, on.Latitude, 1e-4)
	assert.InDelta(t, -97.7729, on.Longitude, 1e-4)
	assert.Equal(t, "Zilker Park", on.Location)

	var off models.RepeaterConfiguration
	require.NoError(t, off.ParseConfig(dmrGatewayRPTC(310999901, 30.2669, -97.7729, false), "v", "c"))
	assert.Zero(t, off.Latitude)
	assert.Zero(t, off.Longitude)
	assert.Equal(t, "Zilker Park", off.Location)
}

func TestConnectedRepeaterPositionsListsOnlyConnectedRepeatersWithAPosition(t *testing.T) {
	t.Parallel()
	db, cleanup := openDB(t)
	defer cleanup()
	require.NoError(t, db.Create(&models.User{ID: 3109999, Callsign: "N0CALL", Username: "owner", Approved: true}).Error)
	require.NoError(t, db.Create(&models.User{ID: 3109998, Callsign: "N1CALL", Username: "other", Approved: true}).Error)
	now := time.Now()
	seed := func(id, owner uint, lat, lon float64, ping time.Duration, kind string) {
		r := models.Repeater{Type: kind, OwnerID: owner, Connected: now.Add(-time.Hour), LastPing: now.Add(-ping),
			RepeaterConfiguration: models.RepeaterConfiguration{ID: id, Callsign: "N0CALL", Latitude: lat, Longitude: lon}}
		require.NoError(t, db.Create(&r).Error)
	}
	seed(310999901, 3109999, 30.2669, -97.7729, 5*time.Second, models.RepeaterTypeMMDVM)
	seed(310999902, 3109999, 0, 0, 5*time.Second, models.RepeaterTypeMMDVM)
	seed(310999903, 3109999, 30.2669, -97.7729, 2*models.RepeaterOnline, models.RepeaterTypeMMDVM)
	seed(310999904, 3109999, 30.2669, -97.7729, 5*time.Second, models.RepeaterTypeIPSC)
	seed(310999801, 3109998, 0, -97.7729, 5*time.Second, models.RepeaterTypeMMDVM)

	ids := func(all bool, user uint) []uint {
		rs, err := models.ConnectedRepeaterPositions(db, user, all, now)
		require.NoError(t, err)
		out := make([]uint, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.ID)
		}
		return out
	}
	assert.Equal(t, []uint{310999901}, ids(false, 3109999))
	assert.Equal(t, []uint{310999801}, ids(false, 3109998))
	assert.Equal(t, []uint{310999801, 310999901}, ids(true, 3109999))
}

func TestARepeaterIsOnlineFromItsLastPingUntilItsSessionEnds(t *testing.T) {
	t.Parallel()
	now := time.Now()
	at := func(d time.Duration) *time.Time {
		v := now.Add(d)
		return &v
	}
	for name, tc := range map[string]struct {
		ping         time.Duration
		never        bool
		disconnected *time.Time
		online       bool
	}{
		"pinged":                  {ping: -5 * time.Second, online: true},
		"never pinged":            {never: true},
		"last ping too old":       {ping: -models.RepeaterOnline},
		"closed after its ping":   {ping: -5 * time.Second, disconnected: at(-time.Second)},
		"closed at its ping":      {ping: -5 * time.Second, disconnected: at(-5 * time.Second)},
		"logged in after a close": {ping: -5 * time.Second, disconnected: at(-time.Minute), online: true},
	} {
		r := models.Repeater{Disconnected: tc.disconnected}
		if !tc.never {
			r.LastPing = now.Add(tc.ping)
		}
		assert.Equal(t, tc.online, r.Online(now), name)
	}
}
