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
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEngine(t *testing.T, keep time.Duration) *wasm.Engine {
	t.Helper()
	e := wasm.NewEngine(wasm.DefaultLimits())
	wasm.KeepFor(e, keep)
	t.Cleanup(func() { _ = e.Close(context.Background()) })
	return e
}

func TestPluginsOfOneModuleShareItsCode(t *testing.T) {
	t.Parallel()
	e := newEngine(t, time.Minute)
	module := guest(t, "echo")
	a, err := e.Start(context.Background(), "a", module, nil)
	require.NoError(t, err)
	t.Cleanup(a.Stop)
	b, err := e.Start(context.Background(), "b", module, nil)
	require.NoError(t, err)
	t.Cleanup(b.Stop)
	assert.True(t, wasm.SameCode(a, b))
	assert.Equal(t, 1, wasm.Compiled(e))
	_, err = e.Start(context.Background(), "c", guest(t, "silent"), nil)
	require.Error(t, err)
	assert.Equal(t, 2, wasm.Compiled(e))
}

func TestAPluginThatStartsAgainUsesTheCodeItHad(t *testing.T) {
	t.Parallel()
	e := newEngine(t, time.Minute)
	module := guest(t, "echo")
	first, err := e.Start(context.Background(), "echo", module, nil)
	require.NoError(t, err)
	first.Stop()
	again, err := e.Start(context.Background(), "echo", module, nil)
	require.NoError(t, err)
	t.Cleanup(again.Stop)
	assert.True(t, wasm.SameCode(first, again))
}

func TestTheCodeOfAModuleThatNoPluginRunsIsFreed(t *testing.T) {
	t.Parallel()
	e := newEngine(t, 50*time.Millisecond)
	inst, err := e.Start(context.Background(), "echo", guest(t, "echo"), nil)
	require.NoError(t, err)
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, 1, wasm.Compiled(e), "kept while the plugin runs")
	inst.Stop()
	require.Eventually(t, func() bool { return wasm.Compiled(e) == 0 }, 5*time.Second, 10*time.Millisecond)
}

func TestTheMeterOfEachPluginSeesItWait(t *testing.T) {
	t.Parallel()
	e := newEngine(t, time.Minute)
	module := guest(t, "echo")
	a, err := e.Start(context.Background(), "a", module, nil)
	require.NoError(t, err)
	t.Cleanup(a.Stop)
	b, err := e.Start(context.Background(), "b", module, nil)
	require.NoError(t, err)
	t.Cleanup(b.Stop)
	require.Eventually(t, func() bool { return wasm.Waiting(a) && wasm.Waiting(b) }, 5*time.Second, 10*time.Millisecond)
}
