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

package testutils

import (
	"testing"

	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/require"
)

func TestSessionPeerTakesOnlyTheDatagramsOfItsMaster(t *testing.T) {
	t.Parallel()
	master := listenLoopback(t)
	foreign := listenLoopback(t)
	p, err := NewSessionPeer(100001, "s3cret", "")
	require.NoError(t, err)
	require.NoError(t, p.Connect(master.LocalAddr().String()))
	t.Cleanup(p.Close)
	_, from := readUDP(t, master)
	closing, err := (&dmrd.Control{Command: dmrd.CommandMSTCL, ID: 100001}).AppendBinary(nil, dmrd.DialectMMDVM)
	require.NoError(t, err)

	_, err = foreign.WriteToUDP(closing, from)
	require.NoError(t, err)
	require.NoError(t, p.Settle())
	require.Zero(t, p.Downs(), "the peer took an MSTCL from a socket that is not its master")

	_, err = master.WriteToUDP(closing, from)
	require.NoError(t, err)
	require.NoError(t, p.Settle())
	require.Equal(t, 1, p.Downs())
}
