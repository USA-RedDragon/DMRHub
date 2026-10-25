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

// TestIPSCSlowDatabaseHoldsNoBurst sends an IPSC call while every database
// statement takes 300 ms, after a call from the same peer ran once. The
// last packet of the call must reach the MMDVM repeater in less than half
// a statement after it was sent.
func TestIPSCSlowDatabaseHoldsNoBurst(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		slow := slowDown(t, stack.DB)
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.StartServers(t)

		mc := testutils.NewMMDVMClient(100001, "", testPassword)
		t.Cleanup(mc.Close)
		require.NoError(t, mc.Connect(stack.MMDVMAddr))
		require.NoError(t, mc.WaitReady(handshakeWait))
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		t.Cleanup(ic.Close)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		require.NoError(t, ic.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		call := func(stream uint32) time.Duration {
			arrived := make(chan time.Time, 1)
			go func() {
				n := 0
				deadline := time.After(30 * time.Second)
				for n < 3 {
					select {
					case p := <-mc.ReceivedPackets():
						if p.Src == 1000001 && p.Dst == 1 {
							n++
						}
					case <-deadline:
						close(arrived)
						return
					}
				}
				arrived <- time.Now()
			}()
			require.NoError(t, ic.SendGroupVoice(1000001, 1, false, stream))
			sent := time.Now()
			last, ok := <-arrived
			require.True(t, ok, "stream %d: the call did not reach the repeater", stream)
			return last.Sub(sent)
		}

		call(12001)
		time.Sleep(time.Second)

		slow.Store(true)
		defer slow.Store(false)
		delay := call(12002)
		t.Logf("the last packet came %v after it was sent, with %v per statement", delay, slowStatement)
		assert.Less(t, delay, slowStatement/2, "an IPSC burst waited on the database")
	})
}
