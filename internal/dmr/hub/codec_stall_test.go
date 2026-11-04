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

package hub_test

import (
	"context"
	"math/rand/v2"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// headerlessCodec2Call returns the voice bursts and the terminator of a
// Codec2 group call to tg without its Voice LC Header, so that its first
// embedded LC decides its codec. Its frames are random, which the hub
// takes as Codec2 2400.
func headerlessCodec2Call(t *testing.T, tg, stream uint, n int) []models.Packet {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, uint32(tg), uint32(testSrcID), layer3.ServiceOptions{})
	require.NoError(t, err)
	lc.SetFID(transcode.Codec2FID)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	vt.Header()
	r := rand.New(rand.NewPCG(uint64(stream), 0xC2)) //nolint:gosec
	out := make([]models.Packet, 0, n+1)
	for i := range n {
		slots := make([][9]byte, layer2.VoiceSlotsPerBurst)
		for j := range slots {
			for k := range 6 {
				slots[j][k] = byte(r.Uint32())
			}
		}
		bursts, err := vt.AddSlots(slots...)
		require.NoError(t, err)
		require.Len(t, bursts, 1)
		p := makeVoicePacket(tg, stream, true, false)
		p.Seq, p.DTypeOrVSeq, p.DMRData = uint(i), uint(i%6), bursts[0]
		if i%6 == 0 {
			p.FrameType = dmrconst.FrameVoiceSync
		}
		out = append(out, p)
	}
	term := makeVoiceTermPacket(tg, stream, true, false)
	term.Seq, term.DTypeOrVSeq, term.DMRData = uint(n), uint(enums.DataTypeTerminatorWithLC), vt.Terminator()
	return append(out, term)
}

// TestAStalledRoutingWorkerKeepsTheCodecOfACall sends a Codec2 call from a
// repeater whose header is lost. Once its embedded LC made it Codec2, the
// routing worker of the repeater stalls, as on a slow database, while the
// rest of the call comes in and the hub prunes the streams that idled.
// The call did not idle, as its bursts came, so its row must stay Codec2,
// with the Codec2 mode that the frames after the stall show.
func TestAStalledRoutingWorkerKeepsTheCodecOfACall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "N0CALL")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedTalkgroup(t, database, 91, "TG91")
		h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		h.ActivateRepeater(context.Background(), testRepeaterID)
		settleDB(t, database)

		const stream = 0xC0DEC
		call := headerlessCodec2Call(t, 91, stream, 6)
		send := func(pkts []models.Packet) {
			for _, p := range pkts {
				h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
				time.Sleep(60 * time.Millisecond)
			}
		}
		send(call[:5])
		synctest.Wait()
		release := h.HoldSource(call[0], models.RepeaterTypeMMDVM)
		send(call[5:])
		time.Sleep(hub.SightedStreamTTL)
		release()
		synctest.Wait()
		settleDB(t, database)

		var row models.Call
		require.NoError(t, database.Where("stream_id = ? AND kind = ?", stream, models.CallKindVoice).First(&row).Error)
		assert.Equal(t, models.CodecCodec2, row.Codec, "the call lost the codec that its embedded LC decided while its bursts waited for routing")
		assert.Equal(t, int(codec2.Mode2400), row.Codec2Mode, "the call lost the Codec2 mode of its frames while its bursts waited for routing")
	})
}
