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

package testutils_test

import (
	"os"
	"strconv"
	"strings"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestFreeUDPPortIsNoPortZeroGives checks that FreeUDPPort picks its
// ports below the range that the kernel gives the sockets that bind port
// 0, which every server of the tests does, so that none of them takes a
// port that a config names before its server binds it, and that it never
// gives a port twice.
func TestFreeUDPPortIsNoPortZeroGives(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("/proc/sys/net/ipv4/ip_local_port_range")
	if err != nil {
		t.Skip("no ip_local_port_range on this system")
	}
	low, err := strconv.Atoi(strings.Fields(string(b))[0])
	require.NoError(t, err)
	seen := map[int]bool{}
	for range 50 {
		p := testutils.FreeUDPPort(t)
		require.Less(t, p, low, "port %d is in the range that port 0 binds take", p)
		require.False(t, seen[p], "port %d was given twice", p)
		seen[p] = true
	}
}
