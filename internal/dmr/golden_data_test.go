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
	"encoding/hex"
	"math"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	anyTone      = 3191868
	otherRadio   = 3140598
	anyToneRpt   = 100001
	otherRpt     = 100002
	goldenTG     = 9
	goldenPeer   = 3129100
	xprPeer      = 314926
	testMaster   = 3149999
	gatewayRptID = 0x00beef01
)

type goldenNet struct {
	stack *testutils.IntegrationStack
	rpt   map[uint32]*testutils.MMDVMClient
	rptID map[uint32]uint
	ob    *testutils.OpenBridgeClient
}

func newGoldenNet(t *testing.T, stack *testutils.IntegrationStack, slot2 bool, ipscPeers ...uint32) *goldenNet {
	t.Helper()
	stack.SeedUser(t, anyTone, "KI5VMF")
	stack.SeedUser(t, otherRadio, "KF5XYZ")
	for _, id := range ipscPeers {
		stack.SeedIPSCRepeater(t, uint(id), anyTone, ipscAuthKey)
	}
	stack.SeedTalkgroup(t, goldenTG, "TG9")
	stack.SeedMMDVMRepeater(t, anyToneRpt, anyTone, testPassword)
	stack.SeedMMDVMRepeater(t, otherRpt, otherRadio, testPassword)
	for _, id := range []uint{anyToneRpt, otherRpt} {
		if slot2 {
			stack.AssignTS2StaticTG(t, id, goldenTG)
		} else {
			stack.AssignTS1StaticTG(t, id, goldenTG)
		}
	}
	stack.StartServers(t)

	n := &goldenNet{
		stack: stack,
		rpt:   map[uint32]*testutils.MMDVMClient{},
		rptID: map[uint32]uint{anyTone: anyToneRpt, otherRadio: otherRpt},
	}
	n.ob = testutils.NewOpenBridgeClient(goldenPeer, obPassword)
	t.Cleanup(n.ob.Close)
	require.NoError(t, n.ob.Connect(stack.OpenBridgeAddr))
	addr := n.ob.LocalAddr()
	stack.SeedPeer(t, goldenPeer, anyTone, obPassword, addr.IP.String(), addr.Port, false, true)
	stack.SeedPeerRule(t, goldenPeer, false, 1, dmrconst.MaxDMRAddress)

	for radio, id := range n.rptID {
		c := testutils.NewMMDVMClient(uint32(id), "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		n.rpt[radio] = c
	}
	keyUp(t, stack, n.rpt[anyTone], anyTone, goldenTG, slot2, 0xA0001)
	keyUp(t, stack, n.rpt[otherRadio], otherRadio, goldenTG, slot2, 0xA0002)
	n.drain(callsArrived([]models.Packet{makeGroupVoiceTermPacket(anyTone, goldenTG, 0xA0001, slot2)}, []models.Packet{makeGroupVoiceTermPacket(otherRadio, goldenTG, 0xA0002, slot2)}))
	return n
}

func (n *goldenNet) drain(done func([][]models.Packet) bool) (map[uint32][]models.Packet, []models.Packet) {
	got := drainUntil(done, n.rpt[anyTone], n.rpt[otherRadio], n.ob)
	return map[uint32][]models.Packet{anyTone: got[0], otherRadio: got[1]}, got[2]
}

func (n *goldenNet) reached(toRadio map[uint32][][]models.Packet, toOB [][]models.Packet) func([][]models.Packet) bool {
	return func(out [][]models.Packet) bool {
		for radio, calls := range toRadio {
			i := map[uint32]int{anyTone: 0, otherRadio: 1}[radio]
			for _, sent := range calls {
				if !gotLast(out[i], sent) {
					return false
				}
			}
		}
		for _, sent := range toOB {
			if !gotLast(out[2], sent) {
				return false
			}
		}
		return true
	}
}

func gotLast(got, sent []models.Packet) bool {
	last := sent[len(sent)-1]
	return slices.ContainsFunc(ofStream(got, last.StreamID), func(p models.Packet) bool { return p.Seq&0xFF == last.Seq&0xFF })
}

func (n *goldenNet) sendAir(t *testing.T, txs []transmission, firstStream uint) [][]models.Packet {
	t.Helper()
	sent := make([][]models.Packet, 0, len(txs))
	for i, tx := range txs {
		if i > 0 {
			gap := tx.at[0] - txs[i-1].at[len(txs[i-1].at)-1]
			time.Sleep(time.Duration(math.Min(math.Max(gap, 100), 3000)) * time.Millisecond)
		}
		c, ok := n.rpt[tx.src]
		require.True(t, ok, "no repeater for source %d", tx.src)
		pkts := tx.packets(firstStream + uint(i))
		sendPackets(t, c.SendDMRD, pkts)
		sent = append(sent, pkts)
	}
	return sent
}

type airGolden struct {
	name    string
	slot2   bool
	dropped []uint32
}

func airGoldens() []airGolden {
	return []airGolden{
		{name: "anytone-v105-phaseA"},
		{name: "anytone-v105-window"},
		{name: "anytone-sms-xpr", slot2: true},
		{name: "anytone-dmr-text", slot2: true},
		{name: "anytone-hsms", slot2: true, dropped: []uint32{dmrconst.MaxDMRAddress}},
		{name: "anytone-aprs", slot2: true, dropped: []uint32{dmrconst.MaxDMRAddress}},
	}
}

// TestGoldenAirCaptures sends the bursts that radios sent on air, as the
// hotspot of each radio would, and checks that the hub delivers each burst
// unchanged to the repeater of the destination and to an OpenBridge peer,
// and that it records each transmission and message (testdata/golden).
// Private traffic to an ID that is not a registered user is dropped
// (decision D4): a BS_Dwn_Act to the all-call ID reaches no repeater.
func TestGoldenAirCaptures(t *testing.T) {
	t.Parallel()
	for _, g := range airGoldens() {
		t.Run(g.name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				n := newGoldenNet(t, stack, g.slot2)
				txs := splitTransmissions(t, readAirCapture(t, "testdata/"+g.name+".txt"), g.slot2)
				sent := n.sendAir(t, txs, 0xB000)
				var routed [][]models.Packet
				toRadio := map[uint32][][]models.Packet{}
				for i, tx := range txs {
					if !isDropped(g.dropped, tx.dst) {
						routed = append(routed, sent[i])
						toRadio[tx.dst] = append(toRadio[tx.dst], sent[i])
					}
				}
				got, ob := n.drain(n.reached(toRadio, routed))

				t.Run("repeaters", func(t *testing.T) {
					t.Parallel()
					needs(t, needRoute()...)
					for i, tx := range txs {
						if isDropped(g.dropped, tx.dst) {
							continue
						}
						assertSamePackets(t, sent[i], ofStream(got[tx.dst], sent[i][0].StreamID), n.rptID[tx.dst], g.slot2)
					}
				})
				t.Run("unregistered", func(t *testing.T) {
					t.Parallel()
					for _, tx := range txs {
						if !isDropped(g.dropped, tx.dst) {
							continue
						}
						for radio, pkts := range got {
							assert.Empty(t, ofCall(pkts, uint(tx.src), uint(tx.dst)), "repeater of %d got traffic to unregistered %d", radio, tx.dst)
						}
					}
				})
				t.Run("openbridge", func(t *testing.T) {
					t.Parallel()
					for i, tx := range txs {
						if isDropped(g.dropped, tx.dst) {
							continue
						}
						assertSamePackets(t, sent[i], ofStream(ob, sent[i][0].StreamID), goldenPeer, false)
					}
				})
				checkGoldenRows(t, recordsOf(stack), g.name, []uint{anyTone, otherRadio}, []uint{anyTone, otherRadio})
			})
		})
	}
}

func isDropped(dropped []uint32, id uint32) bool {
	for _, d := range dropped {
		if d == id {
			return true
		}
	}
	return false
}

func ofStream(got []models.Packet, stream uint) []models.Packet {
	var out []models.Packet
	for _, p := range got {
		if p.StreamID == stream {
			out = append(out, p)
		}
	}
	return out
}

type ipscGolden struct {
	file, tag string
	needs     []string
}

func ipscGoldens() []ipscGolden {
	return []ipscGolden{
		{file: "servers/ipsc/testdata/xpr8400.txt", tag: "datacall"},
		{file: "testdata/xpr8400-data.txt", tag: "dmrtext"},
		{file: "testdata/xpr8400-data.txt", tag: "rate34"},
		{file: "testdata/xpr8400-data.txt", tag: "rate1"},
		{file: "testdata/xpr8400-data.txt", tag: "group34"},
		{file: "testdata/xpr8400-data.txt", tag: "radiocheck"},
		{file: "testdata/xpr8400-data.txt", tag: "callalert"},
		{file: "testdata/xpr8400-data.txt", tag: "callalertack"},
		{file: "testdata/xpr8400-data.txt", tag: "raw1f", needs: []string{"hub-a commit 6"}},
	}
}

// TestGoldenIPSCDataToMMDVM sends the IPSC data calls of an XPR 8400 and of
// a test master to the hub. The repeater of the destination, or of the
// talkgroup, must get every burst that the IPSC packets carry, with the
// payload of each (IPSC carries decoded payloads, so the hub rebuilds the
// bursts), and the hub must record the transmission (testdata/golden).
func TestGoldenIPSCDataToMMDVM(t *testing.T) {
	t.Parallel()
	for _, g := range ipscGoldens() {
		t.Run(g.tag, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				call := readTagged(t, g.file, g.tag)
				if g.tag == "datacall" {
					call = firstCopy(call)
				}
				first, err := ipscwire.DecodeUser(call[0])
				require.NoError(t, err)
				n := newGoldenNet(t, stack, first.Info.Slot() == enums.Slot2, first.ID)

				ic := testutils.NewIPSCClient(first.ID, ipscAuthKey)
				t.Cleanup(ic.Close)
				require.NoError(t, ic.Connect(stack.IPSCAddr))
				require.NoError(t, ic.WaitReady(handshakeWait))

				var want []layer2.RawBurst
				for _, p := range call {
					u, err := ipscwire.DecodeUser(p)
					require.NoError(t, err)
					if raw, err := u.RawBurst(0, layer2.SyncSourceBS); err == nil {
						want = append(want, raw)
					}
					require.NoError(t, ic.SendRaw(p))
					time.Sleep(30 * time.Millisecond)
				}
				group, dst, src := first.Call()
				targets := []uint32{dst}
				if group {
					targets = []uint32{anyTone, otherRadio}
				}
				got, _ := n.drain(func(out [][]models.Packet) bool {
					for _, radio := range targets {
						i, ok := map[uint32]int{anyTone: 0, otherRadio: 1}[radio]
						if ok && len(ofCall(out[i], uint(src), uint(dst))) < len(want) {
							return false
						}
					}
					return true
				})
				t.Run("repeaters", func(t *testing.T) {
					needs(t, join(needRoute(), g.needs)...)
					for _, radio := range targets {
						pkts := ofCall(got[radio], uint(src), uint(dst))
						for _, p := range pkts {
							assert.Equal(t, n.rptID[radio], p.Repeater)
							assert.Equal(t, group, p.GroupCall)
						}
						assertSamePayloads(t, want, burstsOf(pkts), nil)
					}
				})
				checkGoldenRows(t, recordsOf(stack), "ipsc-"+g.tag, []uint{uint(src)}, []uint{uint(dst)})
			})
		})
	}
}

func firstCopy(call [][]byte) [][]byte {
	for i := 1; i < len(call); i++ {
		if hex.EncodeToString(call[i]) == hex.EncodeToString(call[0]) {
			return call[:i]
		}
	}
	return call
}

// TestGoldenDMRGatewayAliasAndGPS sends the talker alias (DMRA) and GPS
// (DMRG) packets that DMRGateway forwards: 15 and 14 octets, cut by
// DMRGateway. DMRHub v1.2.41 crashed on the 15-octet DMRA. The session must
// stay up, and the hub must record the alias and the position of the
// radio (testdata/golden).
func TestGoldenDMRGatewayAliasAndGPS(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, anyTone, "KI5VMF")
		stack.SeedMMDVMRepeater(t, gatewayRptID, anyTone, testPassword)
		stack.StartServers(t)

		gw := testutils.NewMMDVMClient(gatewayRptID, "", testPassword)
		defer gw.Close()
		require.NoError(t, gw.Connect(stack.MMDVMAddr))
		require.NoError(t, gw.WaitReady(handshakeWait))

		for _, fs := range readFields(t, "testdata/dmrgateway-dmra-dmrg.txt") {
			if fs[0] != "dmrgateway" {
				continue
			}
			p, err := hex.DecodeString(fs[2])
			require.NoError(t, err)
			require.NoError(t, gw.SendRaw(p))
			time.Sleep(20 * time.Millisecond)
		}
		require.True(t, gw.Ping(handshakeWait), "the session went down after the DMRA and DMRG packets")
		for n := range 8 {
			for _, cmd := range []string{"DMRA", "DMRG"} {
				short := append([]byte(cmd), make([]byte, n)...)
				require.NoError(t, gw.SendRaw(short))
			}
		}
		require.True(t, gw.Ping(handshakeWait), "the session went down after short DMRA and DMRG packets")

		t.Run("rows", func(t *testing.T) {
			needs(t, join(needRows(), []string{"hub-b commit 11"})...)
			checkGoldenRows(t, recordsOf(stack), "dmrgateway-dmra-dmrg", []uint{anyTone}, nil)
		})
	})
}

// TestGoldenParrotCall replays the parrot call of the interop lane, with
// the times of the capture: two voice LC headers, three superframes and two
// terminators. DMRHub 3b7384a sent a terminator of the reply right after the
// first header. The reply must be one header, the 18 voice bursts in the
// order sent with their octets unchanged, and one terminator at the end.
func TestGoldenParrotCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		needs(t, "parrot fix")
		const repeaterID = 3129101
		stack.SeedUser(t, anyTone, "KI5VMF")
		stack.SeedMMDVMRepeater(t, repeaterID, anyTone, testPassword)
		stack.StartServers(t)
		runParrot(t, stack)

		c := testutils.NewMMDVMClient(repeaterID, "", testPassword)
		defer c.Close()
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))

		var voice []models.Packet
		start := time.Now()
		for _, fs := range readFields(t, "testdata/parrot-dmrhub-inet.txt") {
			at, err := strconv.ParseFloat(fs[0], 64)
			require.NoError(t, err)
			p, err := hex.DecodeString(fs[1])
			require.NoError(t, err)
			pkt, ok := models.UnpackPacket(p)
			require.True(t, ok)
			time.Sleep(time.Until(start.Add(time.Duration(at * float64(time.Millisecond)))))
			require.NoError(t, c.SendRaw(p))
			if pkt.FrameType == dmrconst.FrameVoice || pkt.FrameType == dmrconst.FrameVoiceSync {
				voice = append(voice, pkt)
			}
		}
		require.Len(t, voice, 18)

		reply := ofCall(receiveCall(c, anyTone), parrotID, anyTone)
		require.Len(t, reply, len(voice)+2, "parrot reply packets")
		for _, p := range reply {
			assert.Equal(t, uint(repeaterID), p.Repeater)
			assert.True(t, p.Slot)
			assert.False(t, p.GroupCall)
		}
		assert.Equal(t, dmrconst.FrameDataSync, reply[0].FrameType)
		assert.Equal(t, uint(dmrconst.DTypeVoiceHead), reply[0].DTypeOrVSeq, "the reply must start with the header")
		for i, w := range voice {
			g := reply[i+1]
			assert.Equal(t, w.FrameType, g.FrameType, "voice burst %d frame type", i)
			assert.Equal(t, w.DTypeOrVSeq, g.DTypeOrVSeq, "voice burst %d position", i)
			assert.Equal(t, ambeBits(w.DMRData), ambeBits(g.DMRData), "voice burst %d", i)
		}
		last := reply[len(reply)-1]
		assert.Equal(t, dmrconst.FrameDataSync, last.FrameType)
		assert.Equal(t, uint(dmrconst.DTypeVoiceTerm), last.DTypeOrVSeq, "the reply must end with the terminator")
	})
}

// ambeBits returns the 216 voice bits of a voice burst in hex: the burst
// without the 48 bits of sync or embedded signalling at bits 108 to 155,
// which a parrot reply rebuilds to name the reply.
func ambeBits(b [33]byte) string {
	out := append([]byte{}, b[:13]...)
	out = append(out, b[13]&0xF0, b[19]&0x0F)
	out = append(out, b[20:]...)
	return hex.EncodeToString(out)
}

type appGolden struct {
	name string
	sap  enums.SAP
	data string
}

func appGoldens() []appGolden {
	return []appGolden{
		{name: "tms", sap: enums.SAPIPBasedPacketData, data: "4500003a000d00004011c2140c30b43c0c2febf60fa70fa70026509e001ca00081040d000a004d004f0054004f00200054004500530054000000"},
		{name: "dmrtext", sap: enums.SAPIPBasedPacketData, data: "45000032000c00000111011e0c30b43c0c2febf613981398001edcd6000d000a44004d005200200054004500530054000000"},
		{name: "gps", sap: enums.SAPUDPIPHeaderCompression, data: "0001000000277e277e244750524d432c3230303033372e3030302c412c333431322e373330302c4e2c31303835302e303030302c452c302e30302c3132312e30302c3032313232352c2c2c412a36300d0a"},
	}
}

// TestGoldenAppCaptures sends the user data of real radio packets, from
// dmrgo internal/appcapture (v2.0.0-20261022230539-1f22504946fb), in an
// unconfirmed rate 1/2 packet from the AnyTone to the other radio, and
// checks the delivery and the message rows (testdata/golden). tms is the
// AnyTone M-SMS text "MOTO TEST" and dmrtext the AnyTone text "DMR TEST",
// both as the XPR 8400 relayed them; gps is an AnyTone position report
// received on air on 2026-10-20, a compressed UDP/IPv4 header and an NMEA
// RMC sentence.
func TestGoldenAppCaptures(t *testing.T) {
	t.Parallel()
	for _, g := range appGoldens() {
		t.Run(g.name, func(t *testing.T) {
			t.Parallel()
			forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
				n := newGoldenNet(t, stack, true)
				data, err := hex.DecodeString(g.data)
				require.NoError(t, err)
				var h pdu.DataHeader
				require.NoError(t, h.SetUnconfirmed(pdu.UnconfirmedDataHeader{
					SAP: g.sap, LLIDDestination: otherRadio, LLIDSource: anyTone, FullMessageFlag: true,
				}))
				p, err := packet.NewPacket(h, data, enums.DataTypeRate12)
				require.NoError(t, err)
				c := call{dst: otherRadio, src: anyTone}
				blocks := packetBursts(t, &p)
				tx := transmission{call: c, slot2: true, bursts: append(preambles(t, c, 5, len(blocks)), blocks...)}
				tx.at = make([]float64, len(tx.bursts))
				sent := n.sendAir(t, []transmission{tx}, 0xC000)
				got, _ := n.drain(n.reached(map[uint32][][]models.Packet{otherRadio: sent}, nil))

				t.Run("repeaters", func(t *testing.T) {
					needs(t, needRoute()...)
					assertSamePackets(t, sent[0], ofStream(got[otherRadio], 0xC000), otherRpt, true)
				})
				checkGoldenRows(t, recordsOf(stack), "app-"+g.name, []uint{anyTone}, []uint{otherRadio})
			})
		})
	}
}
