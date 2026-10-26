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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type modemBurst struct {
	control mmdvm.Control
	frame   dmrconst.FrameType
	value   uint
	burst   layer2.RawBurst
}

// modemCall is a group voice call of two superframes from src to the
// talkgroup tg, as the bursts that an MS sends (SyncSourceMS) or a network
// sends (SyncSourceBS).
func modemCall(t *testing.T, src, tg uint32, sync layer2.SyncSource) []modemBurst {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, tg, src, layer3.ServiceOptions{})
	require.NoError(t, err)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: sync})
	require.NoError(t, err)
	out := []modemBurst{{control: mmdvm.ControlDataSync | mmdvm.Control(enums.DataTypeVoiceLCHeader), frame: dmrconst.FrameDataSync,
		value: uint(enums.DataTypeVoiceLCHeader), burst: vt.Header()}}
	frames := make([]vocoder.AMBEFrame, 2*6*layer2.VoiceSlotsPerBurst)
	for i := range frames {
		frames[i] = vocoder.AMBESilence()
	}
	voice, err := vt.AddFrames(frames...)
	require.NoError(t, err)
	for i, b := range voice {
		c, f := mmdvm.Control(i%6), dmrconst.FrameVoice
		if i%6 == 0 {
			c, f = mmdvm.ControlVoiceSync, dmrconst.FrameVoiceSync
		}
		out = append(out, modemBurst{control: c, frame: f, value: uint(i % 6), burst: b})
	}
	return append(out, modemBurst{control: mmdvm.ControlDataSync | mmdvm.Control(enums.DataTypeTerminatorWithLC),
		frame: dmrconst.FrameDataSync, value: uint(enums.DataTypeTerminatorWithLC), burst: vt.Terminator()})
}

// TestModemHotspotCallsBothWays links a simplex modem as repeater 100001
// and an MMDVM network repeater 100002, both on TG1 TS2, and passes a call
// each way.
func TestModemHotspotCallsBothWays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS2StaticTG(t, 100001, 1)
		stack.AssignTS2StaticTG(t, 100002, 1)
		stack.StartServers(t)

		mm := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, mm.Connect(stack.MMDVMAddr))
		t.Cleanup(mm.Close)
		require.NoError(t, mm.WaitReady(handshakeWait))

		cfg := *stack.Config
		m := &cfg.DMR.Modem
		m.Enabled, m.Port, m.RepeaterID, m.RXFrequency, m.TXFrequency = true, "/dev/fake-modem", 100001, 438800000, 438800000
		opened := make(chan *modemtest.Fake, 4)
		srv := modem.NewServerWithOptions(&cfg, stack.Hub, stack.DB, "test", modem.Options{Reply: 200 * time.Millisecond,
			Reopen: 50 * time.Millisecond, Retry: 50 * time.Millisecond, Open: func(string, int) (modem.Port, error) {
				host, dev := net.Pipe()
				opened <- modemtest.New(dev, 1)
				return host, nil
			}})
		require.NoError(t, srv.Start(context.Background()))
		t.Cleanup(func() { _ = srv.Stop(context.Background()) })
		var fake *modemtest.Fake
		select {
		case fake = <-opened:
			t.Cleanup(fake.Close)
		case <-time.After(handshakeWait):
			t.Fatal("the modem was not opened")
		}
		require.True(t, fake.WaitFor(handshakeWait, func(f *modemtest.Fake) bool { return f.Count(mmdvm.CommandGetStatus) >= 2 }))
		time.Sleep(settleDuration)

		var row models.Repeater
		require.NoError(t, stack.DB.First(&row, 100001).Error)
		assert.Equal(t, models.RepeaterTypeModem, row.Type)
		assert.Equal(t, uint(438800000), row.RXFrequency)
		assert.Equal(t, uint(4), row.Slots)
		assert.Equal(t, "DMRHub", row.SoftwareID)

		thief := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, thief.Connect(stack.MMDVMAddr))
		t.Cleanup(thief.Close)
		require.Error(t, thief.WaitReady(time.Second), "a network login took the ID of the modem")

		rf := modemCall(t, 1000001, 1, layer2.SyncSourceMS)
		for _, b := range rf {
			require.NoError(t, fake.SendDMR(enums.Slot2, b.control, b.burst))
			time.Sleep(60 * time.Millisecond)
		}
		pkts := mm.Drain(drainWait)
		require.GreaterOrEqual(t, len(pkts), len(rf), "the network repeater should get the RF call of the modem")
		for _, p := range pkts {
			assert.Equal(t, uint(1), p.Dst)
			assert.Equal(t, uint(1000001), p.Src)
			assert.True(t, p.GroupCall)
			assert.True(t, p.Slot)
			assert.Equal(t, uint(100002), p.Repeater)
		}
		assert.Equal(t, dmrconst.FrameDataSync, pkts[0].FrameType)
		assert.Equal(t, uint(enums.DataTypeVoiceLCHeader), pkts[0].DTypeOrVSeq)

		time.Sleep(4 * time.Second)
		fromNet := modemCall(t, 1000001, 1, layer2.SyncSourceBS)
		for i, b := range fromNet {
			require.NoError(t, mm.SendDMRD(models.Packet{Signature: "DMRD", Seq: uint(i), Src: 1000001, Dst: 1, Repeater: 100002,
				Slot: true, GroupCall: true, FrameType: b.frame, DTypeOrVSeq: b.value, StreamID: 0x7e57, DMRData: b.burst}))
			time.Sleep(60 * time.Millisecond)
		}
		require.True(t, fake.WaitFor(5*time.Second, func(f *modemtest.Fake) bool { return len(f.Transmitted(enums.Slot2)) >= len(fromNet) }),
			"the modem should send the network call on the air")
		fake.WaitFor(0, func(f *modemtest.Fake) bool {
			for i, b := range f.Transmitted(enums.Slot2) {
				if p, d := b.Sync(); d == 0 {
					assert.Contains(t, []enums.SyncPattern{enums.SyncMSSourcedData, enums.SyncMSSourcedVoice}, p, "burst %d", i)
				}
			}
			return true
		})
	})
}
