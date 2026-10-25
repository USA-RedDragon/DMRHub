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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// forAllBackendsWith is forAllBackends with more settings for the stack.
func forAllBackendsWith(t *testing.T, more []testutils.Backend, f func(t *testing.T, stack *testutils.IntegrationStack)) {
	t.Helper()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, testutils.SetupIntegrationStack(t, append([]testutils.Backend{be}, more...)...))
		})
	}
}

// sendCall sends a voice call of n bursts, one each rotationInterval.
func sendCall(t *testing.T, send func(models.Packet) error, src, tg, stream uint, n int) {
	t.Helper()
	next := time.Now()
	for i := range n {
		p := makeGroupVoicePacket(src, tg, stream, false)
		p.Seq = uint(i)
		p.DTypeOrVSeq = dmrconst.VoiceA + uint(i%6)
		if i%6 == 0 {
			p.FrameType = dmrconst.FrameVoiceSync
		}
		require.NoError(t, send(p))
		next = next.Add(rotationInterval)
		time.Sleep(time.Until(next))
	}
}

// TestWebTransportSessionLivesOnThroughAnotherReplica drains the replica
// of a browser hotspot behind a UDPFront, which then sends the QUIC packets
// of the browser to the other replica. The other replica must forward them
// to the replica that holds the QUIC connection and send its answers, so
// that the session goes on: the hotspot keeps its login and gets the calls
// to its talkgroup.
func TestWebTransportSessionLivesOnThroughAnotherReplica(t *testing.T) {
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

		regA.MarkDraining(t.Context())
		front.Drain(stack.WebTransportAddr)
		const bursts = 100
		sendCall(t, talker.SendDMRD, 1000001, 1, 8301, bursts)
		heardMMDVM(browser.Drain(drainWait)).checkN(t, "the browser hotspot", 8301, bursts)
		assert.Positive(t, front.Moves(), "the front did not move the browser")
		assert.Zero(t, browser.Downs(), "the browser hotspot lost its session")
		assert.Equal(t, 1, browser.Ups(), "the browser hotspot logged in again")
	})
}
