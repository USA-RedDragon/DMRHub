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
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestARepeaterGetsTheCallsOfARepeaterInOrderAcrossTopics sends short
// calls from one repeater slot back to back, to TG 1, to TG 2 and to the
// target repeater in turn. The target, which listens on both talkgroups,
// must get the packets in the order they were sent.
func TestARepeaterGetsTheCallsOfARepeaterInOrderAcrossTopics(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedTalkgroup(t, 2, "TG2")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 100002, 2)
		stack.StartServers(t)

		src := testutils.NewMMDVMClient(100001, "", testPassword)
		defer src.Close()
		require.NoError(t, src.Connect(stack.MMDVMAddr))
		require.NoError(t, src.WaitReady(handshakeWait))
		dst := testutils.NewMMDVMClient(100002, "", testPassword)
		defer dst.Close()
		require.NoError(t, dst.Connect(stack.MMDVMAddr))
		require.NoError(t, dst.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		type sent struct{ stream, seq uint }
		var want []sent
		for stream := uint(7301); stream <= 7324; stream++ {
			var voice, term models.Packet
			switch stream % 3 {
			case 0:
				voice, term = makeGroupVoicePacket(1000001, 1, stream, false), makeGroupVoiceTermPacket(1000001, 1, stream, false)
			case 1:
				voice, term = makeGroupVoicePacket(1000001, 2, stream, false), makeGroupVoiceTermPacket(1000001, 2, stream, false)
			default:
				voice, term = makePrivateVoicePacket(100002, stream), makePrivateVoiceTermPacket(100002, stream)
			}
			voice.Seq, term.Seq = 0, 1
			for _, pkt := range []models.Packet{voice, term} {
				require.NoError(t, src.SendDMRD(pkt))
				want = append(want, sent{pkt.StreamID, pkt.Seq})
			}
		}

		var got []sent
		deadline := time.After(10 * time.Second)
		for len(got) < len(want) {
			select {
			case p := <-dst.ReceivedPackets():
				got = append(got, sent{p.StreamID, p.Seq})
			case <-deadline:
				t.Fatalf("got %d of %d packets: %v", len(got), len(want), got)
			}
		}
		assert.Equal(t, want, got)
	})
}
