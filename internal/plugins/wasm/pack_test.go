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

package wasm_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

func initialMemory(t *testing.T, module []byte) []byte {
	t.Helper()
	ctx := context.Background()
	rt := wazero.NewRuntimeWithConfig(ctx, wazero.NewRuntimeConfigInterpreter())
	defer func() { _ = rt.Close(ctx) }()
	compiled, err := rt.CompileModule(ctx, module)
	require.NoError(t, err)
	wasi_snapshot_preview1.MustInstantiate(ctx, rt)
	mod, err := rt.InstantiateModule(ctx, compiled, wazero.NewModuleConfig().WithStartFunctions())
	require.NoError(t, err)
	mem, ok := mod.Memory().Read(0, mod.Memory().Size())
	require.True(t, ok)
	return bytes.Clone(mem)
}

func dataSegments(t *testing.T, module []byte) int {
	t.Helper()
	pos := 8
	for pos < len(module) {
		id := module[pos]
		size, n := uvarint(module[pos+1:])
		body := pos + 1 + n
		if id == 11 {
			count, _ := uvarint(module[body:])
			return int(count)
		}
		pos = body + int(size)
	}
	return 0
}

func uvarint(b []byte) (uint64, int) {
	var v uint64
	for i, c := range b {
		v |= uint64(c&0x7f) << (7 * i)
		if c&0x80 == 0 {
			return v, i + 1
		}
	}
	return 0, 0
}

func TestPackDataKeepsTheMemoryOfAGoPlugin(t *testing.T) {
	t.Parallel()
	module := guest(t, "echo")
	packed := wasm.PackData(module)
	before, after := dataSegments(t, module), dataSegments(t, packed)
	t.Logf("data segments: %d, packed: %d", before, after)
	assert.Less(t, after*10, before, "far fewer segments")
	assert.Equal(t, initialMemory(t, module), initialMemory(t, packed))
}

func module(sections ...[]byte) []byte {
	out := []byte("\x00asm\x01\x00\x00\x00")
	for _, s := range sections {
		out = append(out, s...)
	}
	return out
}

func section(id byte, body ...byte) []byte {
	return append([]byte{id, byte(len(body))}, body...)
}

func TestPackDataJoinsSegmentsInOrder(t *testing.T) {
	t.Parallel()
	memory := section(5, 1, 0, 1)
	data := section(11, 3,
		0, 0x41, 4, 0x0b, 2, 1, 2,
		0, 0x41, 8, 0x0b, 1, 3,
		0, 0x41, 0x80, 0x02, 0x0b, 1, 4)
	got := wasm.PackData(module(memory, data))
	assert.Equal(t, module(memory, section(11, 2,
		0, 0x41, 4, 0x0b, 5, 1, 2, 0, 0, 3,
		0, 0x41, 0x80, 0x02, 0x0b, 1, 4)), got)
	assert.Equal(t, initialMemory(t, module(memory, data)), initialMemory(t, got))
}

func TestPackDataSortsTheSegments(t *testing.T) {
	t.Parallel()
	memory := section(5, 1, 0, 1)
	data := section(11, 2, 0, 0x41, 8, 0x0b, 1, 1, 0, 0x41, 4, 0x0b, 1, 3)
	got := wasm.PackData(module(memory, data))
	assert.Equal(t, module(memory, section(11, 1, 0, 0x41, 4, 0x0b, 5, 3, 0, 0, 0, 1)), got)
	assert.Equal(t, initialMemory(t, module(memory, data)), initialMemory(t, got))
}

func TestPackDataLeavesWhatItCannotPack(t *testing.T) {
	t.Parallel()
	memory := section(5, 1, 0, 1)
	for name, m := range map[string][]byte{
		"overlap":  module(memory, section(11, 2, 0, 0x41, 4, 0x0b, 2, 1, 2, 0, 0x41, 5, 0x0b, 1, 3)),
		"passive":  module(memory, section(11, 2, 1, 1, 1, 0, 0x41, 4, 0x0b, 1, 3)),
		"count":    module(memory, section(12, 2), section(11, 2, 0, 0x41, 4, 0x0b, 1, 1, 0, 0x41, 6, 0x0b, 1, 3)),
		"global":   module(memory, section(11, 2, 0, 0x23, 0, 0x0b, 1, 1, 0, 0x41, 6, 0x0b, 1, 3)),
		"short":    module(memory, []byte{11, 9, 1}),
		"trailing": module(memory, section(11, 1, 0, 0x41, 4, 0x0b, 1, 1, 9)),
		"no data":  module(memory),
		"empty":    {},
	} {
		assert.Equal(t, m, wasm.PackData(m), name)
	}
}
