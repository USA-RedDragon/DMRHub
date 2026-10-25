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

package dmr_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestWebTransportMovesFromADrainingReplica drains the replica of a
// browser hotspot behind a UDPFront while a call to its talkgroup goes on.
// The hotspot must move to the other replica with the token of the drain
// notice, with no new login, and get every burst of the call once.
func TestWebTransportMovesFromADrainingReplica(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100004, 1)
		regA := testutils.Register(t, stack.Hub, stack.KV)
		b := stack.SpawnFullReplica(t)
		testutils.Register(t, b.Hub, stack.KV)
		stack.StartServers(t)
		require.NotEmpty(t, stack.WebTransportAddr)

		front := testutils.NewUDPFront(t)
		front.Add(t, stack.WebTransportAddr)
		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		require.NoError(t, browser.Connect(front.Addr()))
		t.Cleanup(browser.Close)
		require.NoError(t, browser.WaitReady(handshakeWait))
		front.Add(t, b.WebTransportAddr)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, talker.Connect(b.MMDVMAddr))
		t.Cleanup(talker.Close)
		require.NoError(t, talker.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		const bursts = 80
		next := time.Now()
		for i := range bursts {
			p := makeGroupVoicePacket(1000001, 1, 8201, false)
			p.Seq = uint(i)
			p.DTypeOrVSeq = dmrconst.VoiceA + uint(i%6)
			if i%6 == 0 {
				p.FrameType = dmrconst.FrameVoiceSync
			}
			require.NoError(t, talker.SendDMRD(p))
			if i == 15 {
				regA.MarkDraining(t.Context())
				front.Drain(stack.WebTransportAddr)
				stack.MMDVMServer.DrainWebTransport(t.Context())
			}
			next = next.Add(rotationInterval)
			time.Sleep(time.Until(next))
		}
		heardMMDVM(browser.Drain(drainWait)).checkN(t, "the browser hotspot", 8201, bursts)
		assert.Equal(t, 1, browser.Moves(), "the browser hotspot did not move")
		assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
		assert.Equal(t, 1, browser.Ups(), "the browser hotspot logged in again")
	})
}
