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

package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOverlayStampsPacketsWithTheDemoClock writes the overlay of the
// screenshot build. The kernel stamps each packet with the wall clock,
// which the clock patch does not move, so the overlay must swap the file
// that asks for kernel stamps for the one without them: DMRHub then
// stamps each packet with the demo clock when it reads it.
func TestOverlayStampsPacketsWithTheDemoClock(t *testing.T) {
	t.Parallel()
	goroot, err := exec.CommandContext(t.Context(), "go", "env", "GOROOT").Output()
	require.NoError(t, err)
	root, err := filepath.Abs(filepath.Join("..", ".."))
	require.NoError(t, err)
	dir := t.TempDir()
	require.NoError(t, writeOverlay(strings.TrimSpace(string(goroot)), root, dir))
	data, err := os.ReadFile(filepath.Join(dir, "overlay.json"))
	require.NoError(t, err)
	var o struct{ Replace map[string]string }
	require.NoError(t, json.Unmarshal(data, &o))
	file, ok := o.Replace[filepath.Join(root, kernelStamps)]
	require.True(t, ok, "the overlay keeps the kernel stamps")
	src, err := os.ReadFile(file)
	require.NoError(t, err)
	assert.Contains(t, string(src), "func StampArrivals(*net.UDPConn) bool { return false }")
	assert.NotContains(t, string(src), "//go:build")
}
