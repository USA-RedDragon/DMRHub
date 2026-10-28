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
// repeater while Redis holds every call on the ownership records of the
// repeaters until the call ended, and another IPSC peer registers
// meanwhile, which claims its record. No burst of the call may wait on
// Redis for it, so the repeater gets the whole call while Redis holds.
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

		const bursts = 16
		send := func(stream uint32, during func()) {
			start := time.Now()
			for i := range bursts {
				time.Sleep(time.Until(start.Add(time.Duration(i) * burstPeriod)))
				require.NoError(t, talker.SendGroupVoice(1000001, 1, false, stream))
				if i == 3 && during != nil {
					go during()
				}
			}
		}
		count := func() int {
			n := 0
			for {
				got := listener.Drain(drainWait)
				if len(got) == 0 {
					return n
				}
				for _, p := range got {
					if p.Src == 1000001 {
						n++
					}
				}
			}
		}
		send(5301, nil)
		want := count()
		require.Positive(t, want, "the call reached the repeater")

		slow.hold()
		joiner := testutils.NewIPSCClient(200002, ipscAuthKey)
		t.Cleanup(joiner.Close)
		send(5302, func() { _ = joiner.Connect(stack.IPSCAddr) })
		got := count()
		held := slow.held.Load()
		slow.release()
		assert.Equal(t, want, got, "a burst of the call waited on Redis for another peer")
		assert.Positive(t, held, "the other peer did not wait on Redis while the call went on")
		assert.NoError(t, joiner.WaitReady(handshakeWait), "the other peer registered")
	})
}
