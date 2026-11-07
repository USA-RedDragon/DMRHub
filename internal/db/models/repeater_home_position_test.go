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
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseHomePositionReadsDMRGatewaysRPTG(t *testing.T) {
	t.Parallel()
	for text, want := range map[string][2]float64{
		fmt.Sprintf("%+08.4f%+09.4f", 30.2669, -97.7729): {30.2669, -97.7729},
		"-33.8568+151.2153":     {-33.8568, 151.2153},
		"+00.0000+000.0000":     {0, 0},
		"+30.2669-097.7729\x00": {30.2669, -97.7729},
	} {
		lat, lon, err := models.ParseHomePosition([]byte(text))
		require.NoError(t, err, "%q", text)
		assert.InDelta(t, want[0], lat, 1e-9, "%q", text)
		assert.InDelta(t, want[1], lon, 1e-9, "%q", text)
	}
	for _, text := range []string{"", "+30.2669", "30.2669 -97.7729", "+x-097.7729", "+91.0000+000.0000", "+00.0000+181.0000", "+NaN+000.0000"} {
		_, _, err := models.ParseHomePosition([]byte(text))
		assert.ErrorIs(t, err, models.ErrInvalidHomePosition, "%q", text)
	}
}
