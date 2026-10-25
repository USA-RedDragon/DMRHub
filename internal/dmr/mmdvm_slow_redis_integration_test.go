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
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestMMDVMSlowRedisHoldsNoOtherRepeater streams a call from one repeater
// to another while Redis takes 500 ms for each call on the records of the
// repeaters. Meanwhile a third repeater pings, a fourth logs out, and an
// unknown address sends a DMRD, which the server checks against the
// records. No burst of the call may wait on Redis for them, and the
// records must still follow the ping and the logout.
func TestMMDVMSlowRedisHoldsNoOtherRepeater(t *testing.T) {
	t.Parallel()
	forAllSlowRedisBackends(t, "mmdvm:repeater:", nil, func(t *testing.T, stack *testutils.IntegrationStack, slow *slowRedis) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		for _, id := range []uint{100001, 100002, 100004, 100005} {
			stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		}
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		clients := map[uint32]*testutils.MMDVMClient{}
		for _, id := range []uint32{100001, 100002, 100004, 100005} {
			c := testutils.NewMMDVMClient(id, "", testPassword)
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			require.NoError(t, c.WaitReady(handshakeWait))
			c.PausePings()
			clients[id] = c
		}
		talker, listener, pinger, leaver := clients[100001], clients[100002], clients[100004], clients[100005]
		time.Sleep(settleDuration)

		records := servers.MakeKVClient(stack.KV)
		before, err := records.GetRepeater(context.Background(), 100004)
		require.NoError(t, err)

		var dialer net.Dialer
		stranger, err := dialer.DialContext(t.Context(), "udp", stack.MMDVMAddr)
		require.NoError(t, err)
		t.Cleanup(func() { _ = stranger.Close() })
		control := func(command dmrd.Command, id uint32) []byte {
			p, err := (&dmrd.Control{Command: command, ID: id}).AppendBinary(nil, dmrd.DialectMMDVM)
			require.NoError(t, err)
			return p
		}

		const bursts = 16
		send := func(during bool) func(uint) []time.Time {
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
					if !during {
						continue
					}
					switch i {
					case 2:
						require.NoError(t, pinger.SendRaw(control(dmrd.CommandRPTPING, 100004)))
					case 5:
						stray := makeGroupVoicePacket(1000001, 1, 9999, false)
						stray.Repeater = 100009
						_, err := stranger.Write(stray.Encode())
						require.NoError(t, err)
					case 8:
						require.NoError(t, leaver.SendRaw(control(dmrd.CommandRPTCL, 100005)))
					}
				}
				return sent
			}
		}
		worstBursts(t, 8301, bursts, send(false), listener.ReceivedPackets())
		time.Sleep(200 * time.Millisecond)

		slow.on.Store(true)
		worst := worstBursts(t, 8302, bursts, send(true), listener.ReceivedPackets())
		slow.on.Store(false)
		t.Logf("worst delay of the call with %v per Redis call: %v", slowRedisCall, worst[0])
		assert.Less(t, worst[0], slowRedisCall/2, "a burst of the call waited on Redis for another repeater")

		assert.Eventually(t, func() bool {
			r, err := records.GetRepeater(context.Background(), 100004)
			return err == nil && r.PingsReceived > before.PingsReceived
		}, 5*time.Second, 50*time.Millisecond, "the ping reached the record of the repeater")
		assert.Eventually(t, func() bool {
			_, err := records.GetRepeater(context.Background(), 100005)
			return err != nil
		}, 5*time.Second, 50*time.Millisecond, "the logout removed the record of the repeater")
		_, err = records.GetRepeater(context.Background(), 100001)
		assert.NoError(t, err, "the talker keeps its record")
	})
}
