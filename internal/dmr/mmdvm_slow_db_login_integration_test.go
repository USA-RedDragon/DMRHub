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

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMMDVMSlowDatabaseLoginsHoldNoCall streams a call from one repeater to
// another while three more log in at once, as after a restart of the hub,
// and every database statement takes 300 ms. No burst of the call may wait
// on the database for the logins, and each login must still complete.
func TestMMDVMSlowDatabaseLoginsHoldNoCall(t *testing.T) { //nolint:paralleltest,tparallel // the parallel tests of the package starve the call of CPU for more than half a statement
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		slow := slowDown(t, stack.DB)
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		joiners := []uint32{100004, 100005, 100006}
		for _, id := range append([]uint32{100001, 100002}, joiners...) {
			stack.SeedMMDVMRepeater(t, uint(id), 1000001, testPassword)
		}
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		connect := func(id uint32) *testutils.MMDVMClient {
			c := testutils.NewMMDVMClient(id, "", testPassword)
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			require.NoError(t, c.WaitReady(handshakeWait))
			return c
		}
		talker, listener := connect(100001), connect(100002)
		time.Sleep(settleDuration)

		send := func(bursts int, during func()) func(uint) []time.Time {
			return func(stream uint) []time.Time {
				sent := make([]time.Time, 0, bursts)
				start := time.Now()
				for i := range bursts {
					p := makeGroupVoicePacket(1000001, 1, stream, false)
					p.Seq = uint(i)
					due := start.Add(time.Duration(i) * burstPeriod)
					time.Sleep(time.Until(due))
					sent = append(sent, due)
					require.NoError(t, talker.SendDMRD(p))
					if i == 2 && during != nil {
						during()
					}
				}
				return sent
			}
		}
		worstBursts(t, 8401, 16, send(16, nil), listener.ReceivedPackets())
		time.Sleep(200 * time.Millisecond)

		slow.Store(true)
		defer slow.Store(false)
		var wg sync.WaitGroup
		ready := make(chan error, len(joiners))
		login := func() {
			for _, id := range joiners {
				wg.Go(func() {
					c := testutils.NewMMDVMClient(id, "", testPassword)
					t.Cleanup(c.Close)
					if err := c.Connect(stack.MMDVMAddr); err != nil {
						ready <- err
						return
					}
					start := time.Now()
					err := c.WaitReady(30 * time.Second)
					t.Logf("repeater %d logged in after %v", id, time.Since(start))
					ready <- err
				})
			}
		}
		worst := worstBursts(t, 8402, 100, send(100, login), listener.ReceivedPackets())
		wg.Wait()
		close(ready)
		t.Logf("worst delay of the call with %v per statement while %d repeaters log in: %v", slowStatement, len(joiners), worst[0])
		assert.Less(t, worst[0], slowStatement/2, "a burst of the call waited on the database for a login")
		for err := range ready {
			assert.NoError(t, err, "a repeater could not log in")
		}
	})
}
