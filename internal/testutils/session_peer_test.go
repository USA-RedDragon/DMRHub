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
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
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

// TestHBRPSessionPeerWaitsForASlowMasterAsMMDVMHostDoes starts the login
// of an HBRP peer at a master that does not answer yet, as a master that
// waits on its database, and moves the clock of the peer to just before
// the session timeout of the protocol. The peer must keep its login and
// not log in again, which would end the login that the master works on.
func TestHBRPSessionPeerWaitsForASlowMasterAsMMDVMHostDoes(t *testing.T) {
	t.Parallel()
	master := listenLoopback(t)
	p, err := NewHBRPSessionPeer(100001, "s3cret")
	require.NoError(t, err)
	start := time.Now()
	var elapsed atomic.Int64
	p.clock = func() time.Time { return start.Add(time.Duration(elapsed.Load())) }
	require.NoError(t, p.Connect(master.LocalAddr().String()))
	t.Cleanup(p.Close)
	login, _ := readUDP(t, master)
	require.Equal(t, dmrd.CommandRPTL, dmrd.Classify([]byte(login), dmrd.DialectHBRP))

	elapsed.Store(int64(dmrdsession.TimeoutDefault - time.Second))
	p.tick()
	require.Zero(t, p.Downs(), "the peer gave up a login that the master had not answered within the session timeout")
}
