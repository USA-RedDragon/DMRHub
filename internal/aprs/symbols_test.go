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

package aprs_test

import (
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEachLocationServiceHasItsSymbol(t *testing.T) {
	t.Parallel()
	want := map[uint]aprs.Symbol{
		5050: {'/', '-'}, 5051: {'/', '='}, 5052: {'/', 'F'}, 5053: {'/', 'k'}, 5054: {'/', 'v'},
		5055: {'/', '$'}, 5056: {'/', ';'}, 5057: {'/', '['}, 5058: {'/', 'b'}, 5059: {'/', '>'},
	}
	for service, sym := range want {
		name, ok := aprs.ServiceSymbol(service)
		require.True(t, ok, service)
		got, ok := aprs.LookupSymbol(name)
		require.True(t, ok, name)
		assert.Equal(t, sym, got, service)
	}
	for _, id := range []uint{5049, 5060, 0} {
		_, ok := aprs.ServiceSymbol(id)
		assert.False(t, ok, id)
	}
	assert.Len(t, aprs.SymbolNames(), 10)
	_, ok := aprs.LookupSymbol("ufo")
	assert.False(t, ok)
}

func TestCommentsAndSSIDs(t *testing.T) {
	t.Parallel()
	assert.True(t, aprs.ValidComment(""))
	assert.True(t, aprs.ValidComment("DMR 3191868"))
	assert.True(t, aprs.ValidComment(strings.Repeat("a", 43)))
	assert.False(t, aprs.ValidComment(strings.Repeat("a", 44)))
	assert.False(t, aprs.ValidComment("a|b"))
	assert.False(t, aprs.ValidComment("a~b"))
	assert.False(t, aprs.ValidComment("a\rb"))
	assert.False(t, aprs.ValidComment("café"))
	for _, s := range []string{"", "1", "7", "9", "15"} {
		assert.True(t, aprs.ValidSSID(s), s)
	}
	for _, s := range []string{"0", "16", "07", "-9", "a", "100"} {
		assert.False(t, aprs.ValidSSID(s), s)
	}
}
