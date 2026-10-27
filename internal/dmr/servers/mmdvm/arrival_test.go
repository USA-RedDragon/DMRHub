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

package mmdvm

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestACallIsTimedFromWhenItsBurstsCame reads the bursts of a call of
// 120 ms all at once, as a reader that runs late takes them from the
// socket, each with the time the kernel got it. The call tracker must time
// the call from those times: it must keep the call, and not drop it as a
// key-up of no length.
func TestACallIsTimedFromWhenItsBurstsCame(t *testing.T) {
	t.Parallel()
	const repeaterID, stream = uint32(311880), uint(76001)
	r := newDialectRig(t, uint(repeaterID))
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(r.s.DB, ps, kvStore)
	h := hub.NewHub(r.s.DB, kvStore, ps, ct)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		_ = ps.Close()
		_ = kvStore.Close()
	})
	r.s.hub = h
	r.login(dmrd.DialectMMDVM, repeaterID)

	burst := func(seq uint, frame dmrconst.FrameType, value uint) []byte {
		p := models.Packet{
			Signature:   string(dmrconst.CommandDMRD),
			Seq:         seq,
			Src:         1000001,
			Dst:         1,
			Repeater:    uint(repeaterID),
			GroupCall:   true,
			FrameType:   frame,
			DTypeOrVSeq: value,
			StreamID:    stream,
			BER:         -1,
			RSSI:        -1,
		}
		return p.Encode()
	}
	call := [][]byte{
		burst(0, dmrconst.FrameVoice, dmrconst.VoiceA),
		burst(1, dmrconst.FrameVoice, dmrconst.VoiceB),
		burst(2, dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm)),
	}
	const period = 60 * time.Millisecond
	came := time.Now().Add(-time.Duration(len(call)) * period)
	for i, p := range call {
		r.s.handleFrom(context.Background(), udpEndpoint(r.from), p, came.Add(time.Duration(i)*period))
	}

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = nil
		return r.s.DB.Where("stream_id = ?", stream).Find(&calls).Error == nil && len(calls) == 1 && !calls[0].Active
	}, 10*time.Second, 10*time.Millisecond, "the call tracker did not keep and end the call")
	assert.InDelta(t, time.Duration(len(call)-1)*period, calls[0].Duration, float64(time.Millisecond), "the call must last from its first burst to its terminator")
}
