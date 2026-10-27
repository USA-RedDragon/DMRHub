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
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestTerminateLosesNoInboundBurst stops the replica that a talking
// repeater is on as a rolling restart does, right after the load balancer
// passed it a burst, while the call goes on. A repeater on the other
// replica, which does not move, must get every burst of the call once and
// in order: the replica that stops reads all that reached it before it
// closes its socket.
func TestTerminateLosesNoInboundBurst(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100003, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100003, 1)

		regA := testutils.Register(t, stack.Hub, stack.KV)
		b, _ := stack.SpawnRegisteredReplica(t)
		stack.StartServers(t)
		a := stack.Replica()

		fronts := testutils.Fronts{MMDVM: testutils.NewUDPFront(t)}
		fronts.Add(t, a)
		talker, err := testutils.NewSessionPeer(100001, testPassword, "")
		require.NoError(t, err)
		require.NoError(t, talker.Connect(fronts.MMDVM.Addr()))
		t.Cleanup(talker.Close)
		require.NoError(t, talker.WaitReady(handshakeWait))
		fronts.Add(t, b)
		listener, err := testutils.NewSessionPeer(100003, testPassword, "")
		require.NoError(t, err)
		require.NoError(t, listener.Connect(b.MMDVMAddr))
		t.Cleanup(listener.Close)
		require.NoError(t, listener.WaitReady(handshakeWait))

		heardStream := func(stream uint) []arrival { return arrivalsOf(listener.Heard(), stream) }
		probe := uint(8300)
		require.Eventually(t, func() bool {
			if len(heardStream(probe)) > 0 {
				return true
			}
			probe++
			_ = talker.SendDMRD(makeGroupVoicePacket(1000001, 1, probe, false))
			_ = talker.SendDMRD(makeGroupVoiceTermPacket(1000001, 1, probe, false))
			return false
		}, 2*handshakeWait, 300*time.Millisecond, "the call does not reach the other replica")

		const bursts, stopAt = 60, 10
		reached := make(chan struct{})
		var once sync.Once
		call := rotationCall{src: 1000001, stream: 8401, tg: 1, send: func(p models.Packet) error {
			err := talker.SendDMRD(p)
			if p.Seq == stopAt {
				once.Do(func() { close(reached) })
			}
			return err //nolint:wrapcheck
		}}
		var calls sync.WaitGroup
		call.runN(t, &calls, bursts)
		<-reached
		a.Terminate(t, regA, fronts, rotationDrain)
		calls.Wait()
		require.Equal(t, 1, fronts.MMDVM.Moves(), "the talking repeater did not move to the other replica")
		require.Zero(t, talker.Downs(), "the talking repeater lost its session")

		require.Eventually(t, func() bool { return len(heardStream(call.stream)) >= bursts }, time.Minute, 10*time.Millisecond,
			"the repeater on the other replica did not get every burst")
		checkCall(t, "the repeater on the other replica", heardStream(call.stream), bursts, nil)
	})
}

// runN is run for a call of n bursts.
func (c rotationCall) runN(t *testing.T, wg *sync.WaitGroup, n int) {
	t.Helper()
	wg.Go(func() {
		next := time.Now()
		for i := range n {
			p := makeGroupVoicePacket(c.src, c.tg, c.stream, c.slot)
			if i == n-1 {
				p = makeGroupVoiceTermPacket(c.src, c.tg, c.stream, c.slot)
			}
			p.Seq = uint(i)
			if err := c.send(p); err != nil {
				t.Errorf("stream %d burst %d: %v", c.stream, i, err)
			}
			next = next.Add(rotationInterval)
			time.Sleep(time.Until(next))
		}
	})
}
