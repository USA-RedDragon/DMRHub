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
	"encoding/json"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNestedUsersSerializeAsRefs(t *testing.T) {
	t.Parallel()
	u := models.User{ID: 3191868, Callsign: "KI5VMF", Username: "ki5vmf", Email: "a@example.com", Admin: true, Approved: true}

	tg, err := json.Marshal(models.Talkgroup{ID: 91, Name: "TG91", Admins: []models.User{u}, NCOs: []models.User{u}})
	require.NoError(t, err)
	var gotTG map[string]any
	require.NoError(t, json.Unmarshal(tg, &gotTG))
	ref := map[string]any{"id": float64(3191868), "callsign": "KI5VMF"}
	assert.Equal(t, []any{ref}, gotTG["admins"])
	assert.Equal(t, []any{ref}, gotTG["ncos"])
	assert.Equal(t, "TG91", gotTG["name"])

	rpt, err := json.Marshal(models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311860, Callsign: "KI5VMF"}, Owner: u})
	require.NoError(t, err)
	var gotRpt map[string]any
	require.NoError(t, json.Unmarshal(rpt, &gotRpt))
	assert.Equal(t, ref, gotRpt["owner"])
	assert.EqualValues(t, 311860, gotRpt["id"])
	assert.Equal(t, "KI5VMF", gotRpt["callsign"])
}
