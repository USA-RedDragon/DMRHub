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

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestIPSCSlowRedisHoldsNoOtherPeer streams a call from one IPSC peer to a
// repeater while Redis takes 500 ms for each call on the ownership records
// of the repeaters, and another IPSC peer registers meanwhile, which
// claims its record. No burst of the call may wait on Redis for it.
func TestIPSCSlowRedisHoldsNoOtherPeer(t *testing.T) {
	t.Parallel()
	forAllSlowRedisBackends(t, "dmrhub:peer_owner:", nil, func(t *testing.T, stack *testutils.IntegrationStack, slow *slowRedis) {
		require.NoError(t, stack.Hub.SetInstanceID("replica-one"))
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.SeedIPSCRepeater(t, 200002, 1000001, ipscAuthKey)
		stack.StartServers(t)

		listener := testutils.NewMMDVMClient(100002, "", testPassword)
		t.Cleanup(listener.Close)
		require.NoError(t, listener.Connect(stack.MMDVMAddr))
		require.NoError(t, listener.WaitReady(handshakeWait))
		talker := testutils.NewIPSCClient(200001, ipscAuthKey)
		t.Cleanup(talker.Close)
		require.NoError(t, talker.Connect(stack.IPSCAddr))
		require.NoError(t, talker.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		call := func(stream uint32, during func()) time.Duration {
			const bursts = 16
			arrived := make(chan time.Time, 64)
			done := make(chan struct{})
			go func() {
				defer close(arrived)
				for {
					select {
					case p := <-listener.ReceivedPackets():
						if p.StreamID == uint(stream) || p.Src == 1000001 {
							arrived <- time.Now()
						}
					case <-done:
						return
					}
				}
			}()
			start := time.Now()
			for i := range bursts {
				time.Sleep(time.Until(start.Add(time.Duration(i) * burstPeriod)))
				require.NoError(t, talker.SendGroupVoice(1000001, 1, false, stream))
				if i == 3 && during != nil {
					go during()
				}
			}
			time.Sleep(time.Until(start.Add(bursts*burstPeriod + slowRedisCall*3)))
			close(done)
			var worst time.Duration
			last := start
			n := 0
			for at := range arrived {
				worst = max(worst, at.Sub(last))
				last = at
				n++
			}
			require.Positive(t, n, "the call reached the repeater")
			return worst
		}
		call(5301, nil)
		time.Sleep(200 * time.Millisecond)

		slow.on.Store(true)
		defer slow.on.Store(false)
		joiner := testutils.NewIPSCClient(200002, ipscAuthKey)
		t.Cleanup(joiner.Close)
		gap := call(5302, func() { _ = joiner.Connect(stack.IPSCAddr) })
		t.Logf("longest gap between bursts of the call with %v per Redis call: %v", slowRedisCall, gap)
		assert.Less(t, gap, burstPeriod+slowRedisCall/2, "a burst of the call waited on Redis for another peer")
		assert.NoError(t, joiner.WaitReady(handshakeWait), "the other peer registered")
	})
}
