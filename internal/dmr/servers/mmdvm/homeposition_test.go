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

package mmdvm

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func (r *dialectRig) homePosition(id uint32, text string) {
	r.t.Helper()
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTG, ID: id, Text: []byte(text)})
	r.ping(id)
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTPONG)
	r.handled(r.s)
}

func (r *dialectRig) storedPosition(id uint) (float64, float64) {
	r.t.Helper()
	var row models.Repeater
	require.NoError(r.t, r.s.DB.First(&row, id).Error)
	return row.Latitude, row.Longitude
}

func TestAnRPTGMovesTheRepeater(t *testing.T) {
	t.Parallel()
	const id = 310999901
	r := newDialectRig(t, id)
	r.login(dmrd.DialectMMDVM, id)
	lat, lon := r.storedPosition(id)
	require.Zero(t, lat)
	require.Zero(t, lon)

	r.homePosition(id, "+30.2669-097.7729")
	lat, lon = r.storedPosition(id)
	assert.InDelta(t, 30.2669, lat, 1e-9)
	assert.InDelta(t, -97.7729, lon, 1e-9)

	for _, bad := range []string{"", "+30.2669", "garbage", "+91.0000-097.7729", "+30.2669-181.0000"} {
		r.homePosition(id, bad)
		lat, lon = r.storedPosition(id)
		assert.InDelta(t, 30.2669, lat, 1e-9, "%q", bad)
		assert.InDelta(t, -97.7729, lon, 1e-9, "%q", bad)
	}
}
