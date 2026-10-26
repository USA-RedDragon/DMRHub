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

//go:build linux

package wasm

import (
	"testing"
	"unsafe"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMappedMemoryGrowsInPlaceUpToItsMost(t *testing.T) {
	t.Parallel()
	m := mapMemory(pageSize, 4*pageSize)
	t.Cleanup(m.Free)
	first := m.Reallocate(pageSize)
	require.Len(t, first, pageSize)
	first[0] = 7
	grown := m.Reallocate(3 * pageSize)
	require.Len(t, grown, 3*pageSize)
	assert.Equal(t, unsafe.SliceData(first), unsafe.SliceData(grown), "it does not move")
	assert.Equal(t, byte(7), grown[0])
	assert.Equal(t, byte(0), grown[3*pageSize-1])
	assert.Len(t, m.Reallocate(4*pageSize), 4*pageSize)
	assert.Nil(t, m.Reallocate(4*pageSize+1), "never past its most")
}

func TestMappedMemoryIsFreedOnce(t *testing.T) {
	t.Parallel()
	m := mapMemory(0, pageSize)
	m.Free()
	m.Free()
	assert.Nil(t, m.Reallocate(1))
}
