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

package apimodels_test

import (
	"encoding/json"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheAPRSPasscodeIsKeptUnlessGiven(t *testing.T) {
	t.Parallel()
	current := config.Config{}
	current.DMR.Positions.APRSIS = config.APRSIS{Callsign: "N0HUB-10", Passcode: "12345"}

	var req apimodels.POSTConfig
	require.NoError(t, json.Unmarshal([]byte(`{"dmr":{"positions":{"aprs-is":{"callsign":"N0HUB-11"}}}}`), &req))
	var next config.Config
	req.ToConfig(&next, &current)
	assert.Equal(t, "N0HUB-11", next.DMR.Positions.APRSIS.Callsign)
	assert.Equal(t, "12345", next.DMR.Positions.APRSIS.Passcode, "an empty form keeps the passcode")

	require.NoError(t, json.Unmarshal([]byte(`{"aprs-is-passcode":"23456"}`), &req))
	req.ToConfig(&next, &current)
	assert.Equal(t, "23456", next.DMR.Positions.APRSIS.Passcode)
}

func TestTheConfigResponseNeverHoldsTheAPRSPasscode(t *testing.T) {
	t.Parallel()
	cfg := config.Config{}
	cfg.DMR.Positions.APRSIS.Passcode = "12345"
	data, err := json.Marshal(apimodels.ConfigResponse{Config: cfg, Secrets: apimodels.SecretStatus{APRSPasscodeSet: true}})
	require.NoError(t, err)
	assert.NotContains(t, string(data), "12345")
	assert.Contains(t, string(data), `"aprsPasscodeSet":true`)
}
