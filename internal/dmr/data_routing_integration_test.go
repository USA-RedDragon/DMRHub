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
	"math/rand/v2"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// burstLatencyLimit is how soon a burst must reach the other repeater.
const burstLatencyLimit = raceSlowdown * 150 * time.Millisecond

func makeBurst(src, dst, streamID uint, group bool, frame dmrconst.FrameType, value uint, seq uint) models.Packet {
	p := models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Seq:         seq,
		Src:         src,
		Dst:         dst,
		GroupCall:   group,
		FrameType:   frame,
		DTypeOrVSeq: value,
		StreamID:    streamID,
		BER:         -1,
		RSSI:        -1,
	}
	for i := range p.DMRData {
		p.DMRData[i] = byte(rand.IntN(256)) //nolint:gosec
	}
	return p
}

func dataCall(src, dst, streamID uint, group bool) []models.Packet {
	return []models.Packet{
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeCSBK), 0),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeDataHeader), 1),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeRate12), 2),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeRate34), 3),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeRate1), 4),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeUnifiedSingleBlock), 5),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, uint(enums.DataTypeIdle), 6),
		makeBurst(src, dst, streamID, group, dmrconst.FrameDataSync, 13, 7),
		makeBurst(src, dst, streamID, group, 3, 0, 8),
	}
}

// TestDataBurstsRouteLikeVoice sends a data call of every burst kind, group
// and private to a repeater, through MMDVM. Each burst must reach the other
// repeater on its own as soon as it is sent, with its 33 octets unchanged.
func TestDataBurstsRouteLikeVoice(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		for j, tc := range []struct {
			name  string
			dst   uint
			group bool
		}{
			{"group", 1, true},
			{"private to a repeater", 100002, false},
		} {
			for i, sent := range dataCall(1000001, tc.dst, uint(8000+j), tc.group) {
				start := time.Now()
				require.NoError(t, c1.SendDMRD(sent))
				select {
				case got := <-c2.ReceivedPackets():
					assert.Less(t, time.Since(start), burstLatencyLimit, "%s burst %d", tc.name, i)
					assert.Equal(t, sent.DMRData, got.DMRData, "%s burst %d octets", tc.name, i)
					assert.Equal(t, sent.FrameType, got.FrameType, "%s burst %d", tc.name, i)
					assert.Equal(t, sent.DTypeOrVSeq, got.DTypeOrVSeq, "%s burst %d", tc.name, i)
					assert.Equal(t, tc.group, got.GroupCall, "%s burst %d", tc.name, i)
				case <-time.After(drainWait):
					t.Fatalf("%s burst %d did not arrive", tc.name, i)
				}
				time.Sleep(100 * time.Millisecond)
			}
		}
	})
}
