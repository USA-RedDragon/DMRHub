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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const embCC enums.ColorCode = 9

const (
	embRadio    = 1000001
	embTG       = 1
	embIPSC     = 200001
	embIPSCDst  = 200002
	embMMDVM    = 100001
	embMMDVMDst = 100002
	embOBSrc    = 300001
	embOBDst    = 300002
)

// embCall returns two superframes of a group voice call of embRadio to
// embTG, with color code embCC and the PI bit set in the EMB of bursts B to
// F, between a voice LC header and a terminator.
func embCall(t *testing.T) []models.Packet {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(true, embTG, embRadio, layer3.MotorolaServiceOptions())
	require.NoError(t, err)
	frags := layer2.EncodeEmbeddedLC([9]byte(lc))
	tx := transmission{call: call{group: true, dst: embTG, src: embRadio}}
	data := func(dt enums.DataType) {
		cw, err := lc.Codeword(dt)
		require.NoError(t, err)
		raw, err := layer2.BuildDataBurst(cw, dt, embCC, layer2.SyncSourceMS)
		require.NoError(t, err)
		tx.bursts = append(tx.bursts, raw)
		tx.frame = append(tx.frame, dmrconst.FrameDataSync)
		tx.value = append(tx.value, uint(dt))
	}
	data(enums.DataTypeVoiceLCHeader)
	lcss := []enums.LCSS{enums.LCSSSingleFragment, enums.LCSSFirstFragment, enums.LCSSContinuationFragment,
		enums.LCSSContinuationFragment, enums.LCSSLastFragment, enums.LCSSSingleFragment}
	for range 2 {
		for idx := range 6 {
			pos, _ := enums.VoiceBurstAt(idx)
			var embedded [4]byte
			if idx >= 1 && idx <= 4 {
				embedded = frags[idx-1]
			}
			emb := pdu.EmbeddedSignalling{ColorCode: embCC, PreemptionAndPowerControlIndicator: true, LCSS: lcss[idx]}
			raw, err := layer2.BuildVoiceBurst([3]vocoder.AMBEFrame{}, pos, emb, embedded, layer2.SyncSourceMS)
			require.NoError(t, err)
			frame := dmrconst.FrameVoice
			if idx == 0 {
				frame = dmrconst.FrameVoiceSync
			}
			tx.bursts = append(tx.bursts, raw)
			tx.frame = append(tx.frame, frame)
			tx.value = append(tx.value, uint(idx))
		}
	}
	data(enums.DataTypeTerminatorWithLC)
	return tx.packets(0xE1B)
}

func sendIPSC(t *testing.T, c *testutils.IPSCClient, pkts []models.Packet) {
	t.Helper()
	sendPackets(t, func(p models.Packet) error {
		h := layer2.Hint{Kind: layer2.KindData, DataType: enums.DataType(p.DTypeOrVSeq), HasDataType: true}
		if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
			pos, _ := enums.VoiceBurstAt(int(p.DTypeOrVSeq))
			h = layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}
		}
		return c.SendBurst(p.Src, p.Dst, p.GroupCall, p.Slot, uint32(p.StreamID), layer2.RawBurst(p.DMRData), h, isTerminator(p))
	}, pkts)
}

// assertEMBOfPackets checks that the voice bursts B to F of pkts carry
// embCC and the PI bit in their EMB, and that their voice LC header and
// terminator carry embCC in their slot type.
func assertEMBOfPackets(t *testing.T, pkts []models.Packet) {
	t.Helper()
	var voice, data, headers int
	for _, p := range pkts {
		raw := layer2.RawBurst(p.DMRData)
		switch p.FrameType {
		case dmrconst.FrameVoice:
			voice++
			emb := raw.EMB()
			assert.Equal(t, embCC, emb.ColorCode, "color code in the EMB of voice burst %d (seq %d)", p.DTypeOrVSeq, p.Seq)
			assert.True(t, emb.PreemptionAndPowerControlIndicator, "PI bit in the EMB of voice burst %d (seq %d)", p.DTypeOrVSeq, p.Seq)
		case dmrconst.FrameDataSync:
			data++
			if p.DTypeOrVSeq == uint(enums.DataTypeVoiceLCHeader) {
				headers++
			}
			assert.Equal(t, embCC, raw.SlotType().ColorCode, "color code in the slot type of data type %d (seq %d)", p.DTypeOrVSeq, p.Seq)
		case dmrconst.FrameVoiceSync:
		}
	}
	assert.Equal(t, 10, voice, "voice bursts B to F")
	assert.GreaterOrEqual(t, data, 2, "voice LC header and terminator")
	assert.Positive(t, headers, "voice LC header")
}

// assertEMBOfIPSC checks the same as assertEMBOfPackets in the IPSC packets
// of the call.
func assertEMBOfIPSC(t *testing.T, got []testutils.IPSCBurst) {
	t.Helper()
	var voice, data, headers int
	for _, b := range ipscOfCall(got, call{group: true, dst: embTG, src: embRadio}) {
		u, err := ipscwire.DecodeUser(b.Data)
		require.NoError(t, err)
		if u.IsVoice() {
			if u.Voice.Layout == ipscwire.VoiceLayoutA {
				continue
			}
			voice++
			emb := u.Voice.EMB.EmbeddedSignalling()
			assert.Equal(t, embCC, emb.ColorCode, "color code in the EMB of IPSC packet %d", u.RTP.Seq)
			assert.True(t, emb.PreemptionAndPowerControlIndicator, "PI bit in the EMB of IPSC packet %d", u.RTP.Seq)
			continue
		}
		st, ok := u.Data.SlotType()
		if !ok {
			continue
		}
		data++
		if u.Burst.Type() == ipscwire.BurstType(enums.DataTypeVoiceLCHeader) {
			headers++
		}
		assert.Equal(t, embCC, st.ColorCode(), "color code in the slot type of IPSC packet %d", u.RTP.Seq)
	}
	assert.Equal(t, 10, voice, "voice bursts B to F")
	assert.GreaterOrEqual(t, data, 2, "voice LC header and terminator")
	assert.Positive(t, headers, "voice LC header")
}

type embNet struct {
	ipscSrc, ipscDst   *testutils.IPSCClient
	mmdvmSrc, mmdvmDst *testutils.MMDVMClient
	obSrc, obDst       *testutils.OpenBridgeClient
}

func newEMBNet(t *testing.T) *embNet {
	t.Helper()
	stack := testutils.SetupIntegrationStack(t, testutils.SQLiteMemoryBackend())
	stack.SeedUser(t, embRadio, "N0CALL")
	stack.SeedTalkgroup(t, embTG, "TG1")
	stack.SeedIPSCRepeater(t, embIPSC, embRadio, ipscAuthKey)
	stack.SeedIPSCRepeater(t, embIPSCDst, embRadio, ipscAuthKey)
	stack.SeedMMDVMRepeater(t, embMMDVM, embRadio, testPassword)
	stack.SeedMMDVMRepeater(t, embMMDVMDst, embRadio, testPassword)
	stack.AssignTS1StaticTG(t, embIPSCDst, embTG)
	stack.AssignTS1StaticTG(t, embMMDVMDst, embTG)
	stack.StartServers(t)

	n := &embNet{}
	mmdvm := func(id uint32) *testutils.MMDVMClient {
		c := testutils.NewMMDVMClient(id, "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	ipsc := func(id uint32) *testutils.IPSCClient {
		c := testutils.NewIPSCClient(id, ipscAuthKey)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.IPSCAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	n.mmdvmSrc, n.mmdvmDst = mmdvm(embMMDVM), mmdvm(embMMDVMDst)
	n.ipscSrc, n.ipscDst = ipsc(embIPSC), ipsc(embIPSCDst)

	n.obDst = testutils.NewOpenBridgeClient(embOBDst, obPassword)
	t.Cleanup(n.obDst.Close)
	require.NoError(t, n.obDst.Connect(stack.OpenBridgeAddr))
	addr := n.obDst.LocalAddr()
	stack.SeedPeer(t, embOBDst, embRadio, obPassword, addr.IP.String(), addr.Port, false, true)
	stack.SeedPeerRule(t, embOBDst, false, 1, dmrconst.MaxDMRAddress)
	stack.SeedPeer(t, embOBSrc, embRadio, obPassword, "127.0.0.1", 0, true, false)
	stack.SeedPeerRule(t, embOBSrc, true, 1, dmrconst.MaxDMRAddress)
	n.obSrc = testutils.NewOpenBridgeClient(embOBSrc, obPassword)
	t.Cleanup(n.obSrc.Close)
	require.NoError(t, n.obSrc.Connect(stack.OpenBridgeAddr))
	return n
}

// TestVoiceKeepsItsEMB sends a group voice call with a color code and the
// PI bit set from each transport that carries voice. Every MMDVM, IPSC and
// OpenBridge target gets bursts B to F with that color code and PI bit in
// their EMB, and the header and terminator with that color code.
func TestVoiceKeepsItsEMB(t *testing.T) {
	t.Parallel()
	ended := func(c *testutils.IPSCClient) []testutils.IPSCBurst {
		return c.Receive(deliverWait, ipscCallEnds(call{group: true, dst: embTG, src: embRadio}))
	}
	ob := func(c *testutils.OpenBridgeClient) []models.Packet {
		return testutils.Collect(c.ReceivedPackets(), deliverWait, callEnds(embTG))
	}
	for _, tc := range []struct {
		name string
		send func(t *testing.T, n *embNet, pkts []models.Packet)
	}{
		{name: "ipsc", send: func(t *testing.T, n *embNet, pkts []models.Packet) { sendIPSC(t, n.ipscSrc, pkts) }},
		{name: "mmdvm", send: func(t *testing.T, n *embNet, pkts []models.Packet) { sendPackets(t, n.mmdvmSrc.SendDMRD, pkts) }},
		{name: "openbridge", send: func(t *testing.T, n *embNet, pkts []models.Packet) {
			sendPackets(t, func(p models.Packet) error {
				p.Repeater = embOBSrc
				return n.obSrc.SendRawPacket(p)
			}, pkts)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			n := newEMBNet(t)
			tc.send(t, n, embCall(t))
			t.Run("to-mmdvm", func(t *testing.T) {
				t.Parallel()
				assertEMBOfPackets(t, receiveCall(n.mmdvmDst, embTG))
			})
			t.Run("to-ipsc", func(t *testing.T) {
				t.Parallel()
				assertEMBOfIPSC(t, ended(n.ipscDst))
			})
			if tc.name != "openbridge" {
				t.Run("to-openbridge", func(t *testing.T) {
					t.Parallel()
					assertEMBOfPackets(t, ob(n.obDst))
				})
			}
		})
	}
}
