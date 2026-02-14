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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

func FuzzRepeaterUnmarshalMsg(f *testing.F) {
	good := models.Repeater{}
	good.Connection = "YES"
	good.IP = "127.0.0.1"
	good.Port = 62031
	good.Salt = 0xDEADBEEF
	good.PingsReceived = 42
	good.Hotspot = true
	good.Callsign = "N0CALL"
	good.ID = 307201
	encoded, err := good.MarshalMsg(nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte{})
	f.Add([]byte{0x80}) // minimal msgp map
	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		var r models.Repeater
		_, _ = r.UnmarshalMsg(data)
	})
}
