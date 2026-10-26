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

package builtin_test

import (
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/plugins/builtin"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheBuiltinPluginsAreTheParrotAndTheLocationServer(t *testing.T) {
	t.Parallel()
	all := builtin.All()
	require.Len(t, all, 2)
	assert.Equal(t, "parrot", all[0].Key)
	assert.Equal(t, "Parrot", all[0].Name)
	require.Len(t, all[0].Ranges, 1)
	assert.Equal(t, uint(9990), all[0].Ranges[0].First)
	assert.Equal(t, uint(9990), all[0].Ranges[0].Last)
	assert.Equal(t, "locator", all[1].Key)
	assert.Equal(t, "Location server", all[1].Name)
	require.Len(t, all[1].Ranges, 1)
	assert.Equal(t, uint(5050), all[1].Ranges[0].First)
	assert.Equal(t, uint(5059), all[1].Ranges[0].Last)
}

func TestEachBuiltinModuleSaysWhoItIs(t *testing.T) {
	t.Parallel()
	engine := wasm.NewCachedEngine(wasm.DefaultLimits())
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	for _, p := range builtin.All() {
		module, err := p.Module()
		require.NoError(t, err, p.Key)
		inst, err := engine.Start(context.Background(), p.Key, module, nil, p.Args()...)
		require.NoError(t, err, p.Key)
		inst.Stop()
		hello := inst.Hello()
		assert.Equal(t, p.Name, hello.GetName(), p.Key)
		assert.NotEmpty(t, hello.GetDescription(), p.Key)
		require.Len(t, hello.GetIds(), len(p.Ranges), p.Key)
		for i, r := range p.Ranges {
			assert.Equal(t, uint32(r.First), hello.GetIds()[i].GetFirst(), p.Key) //nolint:gosec // DMR IDs
			assert.Equal(t, uint32(r.Last), hello.GetIds()[i].GetLast(), p.Key)   //nolint:gosec // DMR IDs
		}
		assert.Empty(t, hello.GetHosts(), "%s reaches no hosts", p.Key)
	}
}

func TestTheBuiltinPluginsShareOneModule(t *testing.T) {
	t.Parallel()
	all := builtin.All()
	first, err := all[0].Module()
	require.NoError(t, err)
	for _, p := range all[1:] {
		module, err := p.Module()
		require.NoError(t, err)
		assert.Equal(t, first, module, p.Key)
	}
}

func TestTheBuiltinModuleRefusesAnUnknownPlugin(t *testing.T) {
	t.Parallel()
	engine := wasm.NewCachedEngine(wasm.DefaultLimits())
	t.Cleanup(func() { _ = engine.Close(context.Background()) })
	module, err := builtin.All()[0].Module()
	require.NoError(t, err)
	_, err = engine.Start(context.Background(), "none", module, nil, "none")
	require.ErrorIs(t, err, wasm.ErrNoHello)
}
