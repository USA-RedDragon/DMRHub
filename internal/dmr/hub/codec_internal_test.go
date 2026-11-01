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

package hub

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const lateStream = uint(0xA0010)

func codec2Call(t *testing.T) (header, voice, term models.Packet) {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, 3999, 1000001, layer3.ServiceOptions{})
	require.NoError(t, err)
	lc.SetFID(transcode.Codec2FID)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	packet := func(raw layer2.RawBurst, frame dmrconst.FrameType, value uint) models.Packet {
		return models.Packet{Signature: string(dmrconst.CommandDMRD), Src: 1000001, Dst: 3999, Repeater: 500001, GroupCall: true,
			FrameType: frame, DTypeOrVSeq: value, StreamID: lateStream, DMRData: raw, BER: -1, RSSI: -1}
	}
	header = packet(vt.Header(), dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader))
	bursts, err := vt.AddSlots(make([][9]byte, layer2.VoiceSlotsPerBurst)...)
	require.NoError(t, err)
	require.Len(t, bursts, 1)
	voice = packet(bursts[0], dmrconst.FrameVoiceSync, 0)
	term = packet(vt.Terminator(), dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC))
	return header, voice, term
}

// TestLateBurstsKeepTheCodecOfTheCall routes a Codec2 call from a peer,
// then a repeated terminator and a late voice burst after the hub pruned
// the streams that idled. Both must keep the codec of the call, or an
// AMBE target gets the Codec2 burst as it came. A terminator that comes
// after the hub forgot the stream takes the codec from its own LC.
func TestLateBurstsKeepTheCodecOfTheCall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		h.RegisterServer(ServerConfig{Name: "openbridge", Role: RolePeer})
		header, voice, term := codec2Call(t)
		observe := func(p models.Packet) uint8 {
			h.observeCodec(t.Context(), &p, "openbridge")
			return p.Codec
		}
		start := h.clock.Now()
		prune := func(after time.Duration) {
			h.pruneCodecStreams(start.Add(after))
		}

		for _, p := range []models.Packet{header, voice, term} {
			require.Equal(t, uint8(transcode.CodecCodec2), observe(p))
		}
		h.endCodecStream(lateStream)

		prune(StreamIdle + time.Millisecond)
		assert.Equal(t, uint8(transcode.CodecCodec2), observe(term), "a repeated terminator after the prune lost the codec of the call")
		assert.Equal(t, uint8(transcode.CodecCodec2), observe(voice), "a late voice burst after the prune lost the codec of the call")

		prune(StreamIdle + streamClaimHold + streamClaimRecheck + 2*time.Millisecond)
		_, known := h.codecStreams.Load(lateStream)
		require.False(t, known, "the hub kept an ended stream past its claim")
		assert.Equal(t, uint8(transcode.CodecCodec2), observe(term), "a terminator of a forgotten stream did not take the codec of its LC")
	})
}
