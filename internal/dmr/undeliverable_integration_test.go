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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestBurstsIPSCCannotCarryAreReported sends an idle burst and a rate 1
// data burst to a talkgroup that an IPSC peer carries. An XPR 8400 does not
// transmit an idle burst, so the hub must report it undeliverable to that
// peer, and the MMDVM repeater on the talkgroup must still get it. Rate 1
// goes to the peer as a raw block.
func TestBurstsIPSCCannotCarryAreReported(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.AssignTS1StaticTG(t, 200001, 1)

		var mu sync.Mutex
		var got []models.Packet
		var targets []string
		stack.Hub.SetUndeliverableFunc(func(p models.Packet, target, _ string) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, p)
			targets = append(targets, target)
		})
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		c2 := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c2.Close()
		require.NoError(t, c2.WaitReady(handshakeWait))
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		defer ic.Close()
		require.NoError(t, ic.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		require.NoError(t, c1.SendDMRD(makeBurst(1000001, 1, 6101, true, dmrconst.FrameDataSync, uint(enums.DataTypeIdle), 0)))
		assert.Len(t, c2.Drain(drainWait), 1, "the MMDVM repeater must get the idle burst")
		assert.Empty(t, ic.Drain(100*time.Millisecond), "the IPSC peer must not get a burst it cannot carry")

		require.NoError(t, c1.SendDMRD(makeBurst(1000001, 1, 6102, true, dmrconst.FrameDataSync, uint(enums.DataTypeRate1), 0)))
		assert.Len(t, c2.Drain(drainWait), 1, "the MMDVM repeater must get the rate 1 burst")
		rate1 := ic.Drain(drainWait)
		if assert.Len(t, rate1, 1, "the IPSC peer gets rate 1 as a raw block") {
			assert.Equal(t, byte(ipscwire.BurstRaw), rate1[0].Data[30])
		}

		mu.Lock()
		defer mu.Unlock()
		require.Len(t, got, 1, "the burst must be reported undeliverable once")
		assert.Equal(t, models.RepeaterTypeIPSC, targets[0])
		assert.Equal(t, uint(200001), got[0].Repeater)
		assert.Equal(t, uint(6101), got[0].StreamID)
	})
}

// TestDataBurstInAVoiceStreamIsMarkedOnItsCall sends a voice call with a
// data burst in place of a voice burst to a talkgroup that an IPSC peer
// carries. The peer cannot take the burst in the voice stream, so the
// Last Heard row of the call must name the peer and the reason.
func TestDataBurstInAVoiceStreamIsMarkedOnItsCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedIPSCRepeater(t, 200001, 1000001, ipscAuthKey)
		stack.AssignTS1StaticTG(t, 200001, 1)
		stack.StartServers(t)

		c1 := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))
		ic := testutils.NewIPSCClient(200001, ipscAuthKey)
		require.NoError(t, ic.Connect(stack.IPSCAddr))
		defer ic.Close()
		require.NoError(t, ic.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		var c pdu.CSBK
		c.LastBlock = true
		require.NoError(t, c.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: 1, SourceAddress: 1000001}))
		csbk, err := layer2.BuildBurst(&c, 1, layer2.SyncSourceMS)
		require.NoError(t, err)
		const stream = 6201
		before := undeliverableCount(t, models.RepeaterTypeIPSC, "data_in_voice")

		require.NoError(t, c1.SendDMRD(makeBurst(1000001, 1, stream, true, dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceHead), 0)))
		inVoice := makeBurst(1000001, 1, stream, true, dmrconst.FrameVoice, 1, 1)
		inVoice.DMRData = csbk
		require.NoError(t, c1.SendDMRD(inVoice))
		time.Sleep(150 * time.Millisecond)
		require.NoError(t, c1.SendDMRD(makeBurst(1000001, 1, stream, true, dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), 2)))

		require.Eventually(t, func() bool {
			var calls []models.Call
			if stack.DB.Where("stream_id = ? AND active = ?", stream, false).Find(&calls).Error != nil || len(calls) != 1 {
				return false
			}
			return slices.Contains(calls[0].Undeliverable, "ipsc:200001:data_in_voice")
		}, 5*time.Second, 50*time.Millisecond, "the Last Heard row must name the IPSC peer and the reason")
		assert.GreaterOrEqual(t, undeliverableCount(t, models.RepeaterTypeIPSC, "data_in_voice"), before+1)
	})
}

func undeliverableCount(t *testing.T, target, reason string) float64 {
	t.Helper()
	metrics.NewMetrics()
	families, err := prometheus.DefaultGatherer.Gather()
	require.NoError(t, err)
	for _, f := range families {
		if f.GetName() != "dmr_undeliverable_bursts_total" {
			continue
		}
		for _, m := range f.GetMetric() {
			labels := map[string]string{}
			for _, l := range m.GetLabel() {
				labels[l.GetName()] = l.GetValue()
			}
			if labels["target"] == target && labels["reason"] == reason {
				return m.GetCounter().GetValue()
			}
		}
	}
	return 0
}
