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
	"fmt"
	"math/rand/v2"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	mxRadio        = 1000001
	mxUser         = 1000002
	mxDataUser     = 1000003
	mxUnregistered = 3999999
	mxTG           = 91
	mxOtherTG      = 92
	mxKeyTG        = 99
	mxSrcRpt       = 100001
	mxTGRpt        = 100002
	mxSessionRpt   = 100003
	mxUserRpt      = 100004
	mxDataRpt      = 100005
	mxTargetRpt    = 100006
	mxReplicaRpt   = 100007
	mxOtherRpt     = 100008
	mxIPSCSrc      = 200001
	mxIPSC         = 200002
	mxIPSCOther    = 200003
	mxOBPeer       = 3129101
	mxOBSrc        = 3129102
)

type mxPayload struct {
	name         string
	kind         string
	sub          string
	ipsc         bool
	noIPSCSource bool
	voice        bool
	format       string
	text         string
	confirm      string
	build        func(t *testing.T, c call) []transmission
}

func smsPayload(name string, f sms.Format, body string, rate enums.DataType) mxPayload {
	return mxPayload{
		name: name, kind: "data", ipsc: true, noIPSCSource: rate == enums.DataTypeRate1,
		format: formatName(f), text: body,
		build: func(t *testing.T, c call) []transmission {
			text := &sms.Text{Format: f, Source: c.src, Destination: c.dst, Group: c.group, Body: []byte(body)}
			if f == sms.FormatDefinedData {
				text.DD = enums.DDISO8859Part1
			}
			return []transmission{smsTransmission(t, text, rate)}
		},
	}
}

func formatName(f sms.Format) string {
	switch f {
	case sms.FormatTMS:
		return "tms"
	case sms.FormatIP:
		return "ip"
	case sms.FormatAnyTone:
		return "anytone"
	case sms.FormatDefinedData:
		return "defined_data"
	case sms.FormatHytera:
		return "hytera"
	}
	return "data"
}

func confirmedPayload(name, body string, nack bool) mxPayload {
	confirm := "ack"
	if nack {
		confirm = "nack-retry"
	}
	return mxPayload{
		name: name, kind: "data", ipsc: true, format: "ip", text: body, confirm: confirm,
		build: func(t *testing.T, c call) []transmission {
			text := &sms.Text{Format: sms.FormatIP, Source: c.src, Destination: c.dst, Confirmed: true, Body: []byte(body)}
			p := smsPacket(t, text, enums.DataTypeRate12)
			data := packetBursts(t, &p)
			tx := transmission{call: c, bursts: append(preambles(t, c, 5, len(data)), data...)}
			ack := responseTransmission(t, &p, packet.ResponseACK)
			if !nack {
				return []transmission{tx, ack}
			}
			return []transmission{tx, responseTransmission(t, &p, packet.ResponseNACKPacketCRC), tx, ack}
		},
	}
}

func csbkPayload(name, sub string, copies int, set func(cs *pdu.CSBK, c call) error) mxPayload {
	return mxPayload{
		name: name, kind: "csbk", sub: sub, ipsc: true,
		build: func(t *testing.T, c call) []transmission {
			var cs pdu.CSBK
			require.NoError(t, set(&cs, c))
			cs.LastBlock = true
			raw := csbkBurst(t, &cs)
			tx := transmission{call: c}
			for range copies {
				tx.bursts = append(tx.bursts, raw)
			}
			return []transmission{tx}
		},
	}
}

func singleBurstPayload(name, kind string, ipsc bool, dt enums.DataType, p func(t *testing.T) [12]byte) mxPayload {
	return mxPayload{
		name: name, kind: kind, ipsc: ipsc,
		build: func(t *testing.T, c call) []transmission {
			raw, err := layer2.BuildDataBurst(p(t), dt, 7, layer2.SyncSourceMS)
			require.NoError(t, err)
			return []transmission{{call: c, bursts: []layer2.RawBurst{raw}}}
		},
	}
}

func randomPayload(name string, frame dmrconst.FrameType, value uint) mxPayload {
	return mxPayload{
		name: name, kind: "unknown",
		build: func(_ *testing.T, c call) []transmission {
			r := rand.New(rand.NewPCG(uint64(frame), uint64(value)))
			var raw layer2.RawBurst
			for i := range raw {
				raw[i] = byte(r.Uint32())
			}
			return []transmission{{call: c, bursts: []layer2.RawBurst{raw}, frame: []dmrconst.FrameType{frame}, value: []uint{value}}}
		},
	}
}

func voicePayload() mxPayload {
	return mxPayload{
		name: "voice", kind: "voice", ipsc: true, voice: true,
		build: func(t *testing.T, c call) []transmission {
			tx := transmission{call: c}
			fields := readFields(t, "testdata/parrot-dmrhub-inet.txt")
			for i, fs := range fields {
				if i == 1 || i == len(fields)-1 {
					continue
				}
				p, err := hex.DecodeString(fs[1])
				require.NoError(t, err)
				pkt, ok := models.UnpackPacket(p)
				require.True(t, ok)
				tx.bursts = append(tx.bursts, layer2.RawBurst(pkt.DMRData))
				tx.frame = append(tx.frame, pkt.FrameType)
				tx.value = append(tx.value, pkt.DTypeOrVSeq)
			}
			return []transmission{tx}
		},
	}
}

func mxPayloads() []mxPayload {
	return []mxPayload{
		voicePayload(),
		smsPayload("sms-ip", sms.FormatIP, "IP TEST", enums.DataTypeRate12),
		smsPayload("sms-anytone", sms.FormatAnyTone, "ANYTONE TEST", enums.DataTypeRate12),
		smsPayload("sms-tms", sms.FormatTMS, "TMS TEST", enums.DataTypeRate12),
		smsPayload("sms-dd", sms.FormatDefinedData, "DD TEST", enums.DataTypeRate12),
		smsPayload("sms-hytera", sms.FormatHytera, "HYTERA TEST", enums.DataTypeRate12),
		smsPayload("sms-rate34", sms.FormatIP, "RATE 3/4 TEST", enums.DataTypeRate34),
		smsPayload("sms-rate1", sms.FormatIP, "RATE 1 TEST", enums.DataTypeRate1),
		confirmedPayload("sms-confirmed-ack", "CONFIRMED TEST", false),
		confirmedPayload("sms-confirmed-nack-retry", "RETRY TEST", true),
		{
			name: "udt", kind: "udt", ipsc: true,
			build: func(t *testing.T, c call) []transmission {
				var h pdu.DataHeader
				require.NoError(t, h.SetUDT(pdu.UDTHeader{
					Group: c.group, SAP: enums.SAPShortData, UDTFormat: enums.UDTISO8,
					LLIDDestination: c.dst, LLIDSource: c.src, Opcode: pdu.UDTOpcodeInboundHeader,
				}))
				hp, err := h.Encode()
				require.NoError(t, err)
				head, err := layer2.BuildDataBurst(hp, enums.DataTypeDataHeader, 7, layer2.SyncSourceMS)
				require.NoError(t, err)
				block, err := layer2.BuildDataBurst([12]byte{'U', 'D', 'T', ' ', 'T', 'E', 'S', 'T'}, enums.DataTypeRate12, 7, layer2.SyncSourceMS)
				require.NoError(t, err)
				return []transmission{{call: c, bursts: []layer2.RawBurst{head, block}}}
			},
		},
		csbkPayload("radio-check", "radio_check", 1, func(cs *pdu.CSBK, c call) error {
			return cs.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: c.src, CheckedAddress: c.dst})
		}),
		csbkPayload("call-alert-repeated", "call_alert", 3, func(cs *pdu.CSBK, c call) error {
			return cs.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: c.dst, SourceAddress: c.src})
		}),
		{
			name: "mbc", kind: "mbc", ipsc: true,
			build: func(t *testing.T, c call) []transmission {
				h := pdu.MBCHeader{}
				require.NoError(t, h.SetPrivateDataGrant(pdu.PrivateDataGrantPDU{PhysicalChannel: 0xFFF, TargetAddress: c.dst, SourceAddress: c.src}))
				hp, err := h.Encode()
				require.NoError(t, err)
				head, err := layer2.BuildDataBurst(hp, enums.DataTypeMBCHeader, 7, layer2.SyncSourceMS)
				require.NoError(t, err)
				cont := pdu.MBCContinuation{LastBlock: true}
				cp, err := cont.Encode()
				require.NoError(t, err)
				tail, err := layer2.BuildDataBurst(cp, enums.DataTypeMBCContinuation, 7, layer2.SyncSourceMS)
				require.NoError(t, err)
				return []transmission{{call: c, bursts: []layer2.RawBurst{head, tail}}}
			},
		},
		singleBurstPayload("usbd", "usbd", false, enums.DataTypeUnifiedSingleBlock, func(t *testing.T) [12]byte {
			u := pdu.UnifiedSingleBlockData{ServiceType: pdu.ServiceTypeManufacturerSpecific1, Payload: [10]byte{1, 2, 3, 4, 5, 6, 7, 8, 9}}
			p, err := u.Encode()
			require.NoError(t, err)
			return p
		}),
		singleBurstPayload("idle", "idle", false, enums.DataTypeIdle, func(*testing.T) [12]byte { return pdu.PRFillBits() }),
		randomPayload("random-octets", dmrconst.FrameDataSync, uint(enums.DataTypeCSBK)),
		randomPayload("frame-type-3", dmrconst.FrameType(3), 0),
	}
}

type mxNet struct {
	stack                         *testutils.IntegrationStack
	tg, user, data, target, other *testutils.MMDVMClient
	ob                            *testutils.OpenBridgeClient
	ipsc, ipscOther               *testutils.IPSCClient
	src                           *testutils.MMDVMClient
	session                       *testutils.SessionPeer
	obSrc                         *testutils.OpenBridgeClient
	ipscSrc                       *testutils.IPSCClient
}

func newMXNet(t *testing.T, stack *testutils.IntegrationStack) *mxNet {
	t.Helper()
	stack.SeedUser(t, mxRadio, "N0CALL")
	stack.SeedUser(t, mxUser, "N1CALL")
	stack.SeedUser(t, mxDataUser, "N2CALL")
	for _, tg := range []uint{mxTG, mxOtherTG, mxKeyTG} {
		stack.SeedTalkgroup(t, tg, fmt.Sprintf("TG%d", tg))
	}
	for id, owner := range map[uint]uint{
		mxSrcRpt: mxRadio, mxTGRpt: mxRadio, mxSessionRpt: mxRadio, mxUserRpt: mxUser,
		mxDataRpt: mxDataUser, mxTargetRpt: mxRadio, mxOtherRpt: mxRadio,
	} {
		stack.SeedMMDVMRepeater(t, id, owner, testPassword)
	}
	for _, id := range []uint{mxIPSCSrc, mxIPSC, mxIPSCOther} {
		stack.SeedIPSCRepeater(t, id, mxRadio, ipscAuthKey)
	}
	stack.AssignTS1StaticTG(t, mxTGRpt, mxTG)
	stack.AssignTS1StaticTG(t, mxIPSC, mxTG)
	stack.AssignTS1StaticTG(t, mxOtherRpt, mxOtherTG)
	stack.StartServers(t)

	n := &mxNet{stack: stack}
	mmdvm := func(id uint) *testutils.MMDVMClient {
		c := testutils.NewMMDVMClient(uint32(id), "", testPassword)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	n.src, n.tg, n.user, n.data = mmdvm(mxSrcRpt), mmdvm(mxTGRpt), mmdvm(mxUserRpt), mmdvm(mxDataRpt)
	n.target, n.other = mmdvm(mxTargetRpt), mmdvm(mxOtherRpt)

	sp, err := testutils.NewSessionPeer(mxSessionRpt, testPassword, "")
	require.NoError(t, err)
	t.Cleanup(sp.Close)
	require.NoError(t, sp.Connect(stack.MMDVMAddr))
	require.NoError(t, sp.WaitReady(handshakeWait))
	n.session = sp

	ipscPeer := func(id uint32) *testutils.IPSCClient {
		c := testutils.NewIPSCClient(id, ipscAuthKey)
		t.Cleanup(c.Close)
		require.NoError(t, c.Connect(stack.IPSCAddr))
		require.NoError(t, c.WaitReady(handshakeWait))
		return c
	}
	n.ipscSrc, n.ipsc, n.ipscOther = ipscPeer(mxIPSCSrc), ipscPeer(mxIPSC), ipscPeer(mxIPSCOther)

	n.ob = testutils.NewOpenBridgeClient(mxOBPeer, obPassword)
	t.Cleanup(n.ob.Close)
	require.NoError(t, n.ob.Connect(stack.OpenBridgeAddr))
	addr := n.ob.LocalAddr()
	stack.SeedPeer(t, mxOBPeer, mxRadio, obPassword, addr.IP.String(), addr.Port, false, true)
	stack.SeedPeerRule(t, mxOBPeer, false, 1, dmrconst.MaxDMRAddress)
	stack.SeedPeer(t, mxOBSrc, mxRadio, obPassword, "127.0.0.1", 0, true, false)
	stack.SeedPeerRule(t, mxOBSrc, true, 1, dmrconst.MaxDMRAddress)
	n.obSrc = testutils.NewOpenBridgeClient(mxOBSrc, obPassword)
	t.Cleanup(n.obSrc.Close)
	require.NoError(t, n.obSrc.Connect(stack.OpenBridgeAddr))
	time.Sleep(settleDuration)

	keyUp(t, n.user, mxUser, mxKeyTG, false, 0xD0001)
	keyUp(t, n.src, mxRadio, mxKeyTG, false, 0xD0002)
	heard := smsTransmission(t, &sms.Text{Format: sms.FormatIP, Source: mxDataUser, Destination: mxRadio, Body: []byte("HEARD")}, enums.DataTypeRate12)
	sendPackets(t, n.data.SendDMRD, heard.packets(0xD0003))
	time.Sleep(drainWait)
	n.drain(500 * time.Millisecond)
	return n
}

type mxGot struct {
	tg, user, data, target, other, ob []models.Packet
	ipsc, ipscOther                   []testutils.IPSCBurst
}

func (n *mxNet) drain(d time.Duration) mxGot {
	var g mxGot
	deadline := time.Now().Add(d + 10*time.Second)
	start := time.Now()
	for {
		var r mxGot
		done := make(chan struct{}, 2)
		go func() { r.ipsc = n.ipsc.Drain(500 * time.Millisecond); done <- struct{}{} }()
		go func() { r.ipscOther = n.ipscOther.Drain(500 * time.Millisecond); done <- struct{}{} }()
		got := drainAll(500*time.Millisecond, n.tg, n.user, n.data, n.target, n.other, n.ob)
		<-done
		<-done
		quiet := len(r.ipsc)+len(r.ipscOther) == 0
		for _, p := range got {
			quiet = quiet && len(p) == 0
		}
		g.tg, g.user, g.data = append(g.tg, got[0]...), append(g.user, got[1]...), append(g.data, got[2]...)
		g.target, g.other, g.ob = append(g.target, got[3]...), append(g.other, got[4]...), append(g.ob, got[5]...)
		g.ipsc, g.ipscOther = append(g.ipsc, r.ipsc...), append(g.ipscOther, r.ipscOther...)
		if (quiet && time.Since(start) >= d) || time.Now().After(deadline) {
			return g
		}
	}
}

type mxSource struct {
	name string
	send func(t *testing.T, n *mxNet, tx transmission, stream uint) []models.Packet
	ipsc bool
}

func mxSources() []mxSource {
	return []mxSource{
		{name: "mmdvm", send: func(t *testing.T, n *mxNet, tx transmission, stream uint) []models.Packet {
			pkts := tx.packets(stream)
			sendPackets(t, n.src.SendDMRD, pkts)
			return pkts
		}},
		{name: "sessionpeer", send: func(t *testing.T, n *mxNet, tx transmission, stream uint) []models.Packet {
			pkts := tx.packets(stream)
			sendPackets(t, n.session.SendDMRD, pkts)
			return pkts
		}},
		{name: "openbridge", send: func(t *testing.T, n *mxNet, tx transmission, stream uint) []models.Packet {
			pkts := tx.packets(stream)
			sendPackets(t, func(p models.Packet) error {
				p.Repeater = mxOBSrc
				return n.obSrc.SendRawPacket(p)
			}, pkts)
			return pkts
		}},
		{name: "ipsc", ipsc: true, send: func(t *testing.T, n *mxNet, tx transmission, stream uint) []models.Packet {
			pkts := tx.packets(stream)
			for i, p := range pkts {
				h := layer2.Hint{Kind: layer2.KindData, DataType: enums.DataType(p.DTypeOrVSeq), HasDataType: true}
				if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
					pos, _ := enums.VoiceBurstAt(int(p.DTypeOrVSeq))
					h = layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}
				}
				require.NoError(t, n.ipscSrc.SendBurst(p.Src, p.Dst, p.GroupCall, p.Slot, uint32(stream), layer2.RawBurst(p.DMRData), h, i == len(pkts)-1))
				time.Sleep(30 * time.Millisecond)
			}
			return pkts
		}},
	}
}

type mxAddr struct {
	name string
	c    call
	only []string
}

func mxAddrs() []mxAddr {
	return []mxAddr{
		{name: "group", c: call{group: true, dst: mxTG, src: mxRadio}},
		{name: "user", c: call{dst: mxUser, src: mxRadio}},
		{name: "data-only-user", c: call{dst: mxDataUser, src: mxRadio}, only: []string{"sms-ip", "radio-check"}},
		{name: "repeater", c: call{dst: mxTargetRpt, src: mxRadio}, only: []string{"sms-ip", "radio-check"}},
		{name: "ipsc-repeater", c: call{dst: mxIPSC, src: mxRadio}, only: []string{"voice", "sms-ip", "radio-check"}},
		{name: "unregistered", c: call{dst: mxUnregistered, src: mxRadio}, only: []string{"voice", "sms-ip", "radio-check"}},
	}
}

func (a mxAddr) takes(p mxPayload) bool {
	if p.confirm != "" && a.name != "user" {
		return false
	}
	if a.only == nil {
		return true
	}
	for _, o := range a.only {
		if o == p.name {
			return true
		}
	}
	return false
}

// TestTransparencyMatrix sends every kind of traffic from every transport
// to every kind of target. Each target that should get a transmission must
// get every burst, with the 33 octets unchanged on MMDVM and OpenBridge
// paths (IPSC carries decoded payloads, so there the payloads must be
// equal), and no other target may get it. IPSC peers get only their
// talkgroups and their own private traffic (decision D1); private traffic
// to an ID that is not a registered user goes nowhere (decision D4); bursts
// that IPSC cannot carry are recorded as undeliverable to it (decision D6).
// The hub must write one transmission row of the right kind for each, and
// one message row with the format and text for each SMS.
func TestTransparencyMatrix(t *testing.T) {
	t.Parallel()
	for _, src := range mxSources() {
		t.Run(src.name, func(t *testing.T) {
			t.Parallel()
			for _, be := range allBackends() {
				t.Run(be.Name, func(t *testing.T) {
					t.Parallel()
					stream := uint(0xE0000)
					for _, addr := range mxAddrs() {
						first := stream
						for _, p := range mxPayloads() {
							if addr.takes(p) && (!src.ipsc || (p.ipsc && !p.noIPSCSource)) {
								stream += 16
							}
						}
						t.Run(addr.name, func(t *testing.T) {
							t.Parallel()
							n := newMXNet(t, testutils.SetupIntegrationStack(t, be))
							stream := first
							for _, p := range mxPayloads() {
								if !addr.takes(p) || (src.ipsc && (!p.ipsc || p.noIPSCSource)) {
									continue
								}
								stream += 16
								runMXCase(t, n, src, addr, p, stream)
							}
						})
					}
				})
			}
		})
	}
}

func runMXCase(t *testing.T, n *mxNet, src mxSource, addr mxAddr, p mxPayload, stream uint) {
	t.Run(p.name, func(t *testing.T) {
		txs := p.build(t, addr.c)
		var sent, heard []models.Packet
		for i, tx := range txs {
			if tx.src == addr.c.src {
				pkts := src.send(t, n, tx, stream+uint(i))
				if i == 0 {
					sent = pkts
				}
			} else {
				heard = append(heard, awaitHeard(n.user, uint(addr.c.src), uint(addr.c.dst), 10*time.Second)...)
				sendPackets(t, n.user.SendDMRD, tx.packets(stream+uint(i)))
			}
			time.Sleep(100 * time.Millisecond)
		}
		total := 0
		for _, tx := range txs {
			total += len(tx.bursts)
		}
		g := n.drain(time.Duration(total) * 60 * time.Millisecond)
		g.user = append(heard, g.user...)
		mine := func(pkts []models.Packet) []models.Packet {
			return ofCall(pkts, uint(addr.c.src), uint(addr.c.dst))
		}
		first := func(pkts []models.Packet) []models.Packet {
			if !src.ipsc {
				return ofStream(mine(pkts), stream)
			}
			return firstStream(mine(pkts))
		}
		route := needRoute()
		if p.voice {
			route = nil
		}
		check := func(name string, pkts []models.Packet, repeater uint, needed ...[]string) {
			t.Run(name, func(t *testing.T) {
				needs(t, join(needed...)...)
				if src.ipsc {
					assertSameDMRDPayloads(t, sent, first(pkts))
					return
				}
				assertSamePackets(t, sent, first(pkts), repeater, false)
			})
		}
		none := func(name string, pkts []models.Packet) {
			t.Run(name, func(t *testing.T) {
				assert.Empty(t, mine(pkts))
			})
		}
		noneIPSC := func(name string, got []testutils.IPSCBurst, needed ...[]string) {
			t.Run(name, func(t *testing.T) {
				needs(t, join(needed...)...)
				assert.Empty(t, ipscOfCall(got, addr.c))
			})
		}
		ipscGets := func(name string, needed ...[]string) {
			t.Run(name, func(t *testing.T) {
				needs(t, join(needed...)...)
				raws := ipscBursts(t, ipscOfCall(g.ipsc, addr.c))
				assertSameDMRDPayloads(t, sent, packetsOf(withoutRepeatedHeaders(raws), sent))
			})
		}

		var leak []string
		if p.voice {
			leak = []string{"hub-a commit 5"}
		}
		switch addr.name {
		case "group":
			check("talkgroup-repeater", g.tg, mxTGRpt, route)
			none("other-talkgroup-repeater", g.other)
			if p.ipsc {
				once := []string{"hub-a commit 5"}
				if p.voice && src.ipsc {
					once = nil
				}
				ipscGets("ipsc-talkgroup-repeater", route, once)
			} else {
				noneIPSC("ipsc-talkgroup-repeater", g.ipsc)
			}
			noneIPSC("ipsc-other-repeater", g.ipscOther, leak)
		case "user":
			check("user-repeater", g.user, mxUserRpt, route)
			none("talkgroup-repeater", g.tg)
			noneIPSC("ipsc-repeaters", append(g.ipsc, g.ipscOther...))
		case "data-only-user":
			check("user-repeater", g.data, mxDataRpt, route, []string{"hub-a commit 3"})
		case "repeater":
			check("repeater", g.target, mxTargetRpt, route)
			none("user-repeater", g.user)
		case "ipsc-repeater":
			ipscGets("ipsc-repeater", route)
			noneIPSC("ipsc-other-repeater", g.ipscOther, leak)
		case "unregistered":
			for name, pkts := range map[string][]models.Packet{"talkgroup-repeater": g.tg, "user-repeater": g.user, "data-only-user-repeater": g.data, "repeater": g.target, "other-repeater": g.other} {
				none(name, pkts)
			}
			noneIPSC("ipsc-repeaters", append(g.ipsc, g.ipscOther...))
		}
		if addr.name != "unregistered" {
			t.Run("openbridge", func(t *testing.T) {
				if src.ipsc {
					assertSameDMRDPayloads(t, sent, first(g.ob))
					return
				}
				assertSamePackets(t, sent, first(g.ob), mxOBPeer, false)
			})
		}

		checkMXRows(t, n, src, addr, p, stream, g)
	})
}

func checkMXRows(t *testing.T, n *mxNet, src mxSource, addr mxAddr, p mxPayload, stream uint, g mxGot) {
	t.Helper()
	rowStream := stream
	if src.ipsc {
		rowStream = deliveredStream(g, addr)
	}
	t.Run("transmission-row", func(t *testing.T) {
		needs(t, needRows()...)
		if !src.ipsc || addr.name != "unregistered" {
			require.NotZero(t, rowStream, "no delivered packet names the stream of the hub")
		}
		want := map[string]any{"kind": p.kind, "source_id": addr.c.src}
		if p.sub != "" {
			want["sub_kind~"] = p.sub
		}
		if src.ipsc && addr.name == "unregistered" {
			assert.Equal(t, 1, countRows(t, n.stack.DB, "calls", "destination_id", addr.c.dst, want), "transmission rows of kind %s", p.kind)
			return
		}
		assert.Equal(t, 1, countRows(t, n.stack.DB, "calls", "stream_id", rowStream, want), "transmission rows of kind %s", p.kind)
		time.Sleep(time.Second)
		assert.Len(t, rowsOf(t, n.stack.DB, "calls", "stream_id", rowStream), 1, "rows of the stream")
	})
	if addr.name == "group" && !p.ipsc {
		t.Run("undeliverable-to-ipsc", func(t *testing.T) {
			needs(t, join(needRows(), []string{"hub-a commit 7"})...)
			requireRows(t, n.stack.DB, "calls", "stream_id", rowStream, map[string]any{"undeliverable+": true})
		})
	}
	if p.text != "" {
		t.Run("message-row", func(t *testing.T) {
			needs(t, needMessages()...)
			want := map[string]any{
				"format": p.format, "text": p.text, "source_id": addr.c.src,
				"destination_id": addr.c.dst, "is_group": addr.c.group,
			}
			if p.confirm != "" {
				want["confirmed"] = true
				want["response"] = "ack"
			}
			assert.Equal(t, 1, countRows(t, n.stack.DB, "messages", "destination_id", addr.c.dst, want), "message rows")
		})
	}
}

func withoutRepeatedHeaders(raws []layer2.RawBurst) []layer2.RawBurst {
	out := make([]layer2.RawBurst, 0, len(raws))
	for _, raw := range raws {
		if n := len(out); n > 0 && raw.SlotType().DataType == enums.DataTypeVoiceLCHeader && out[n-1].SlotType().DataType == enums.DataTypeVoiceLCHeader {
			_, a := payloadOf(raw, false)
			_, b := payloadOf(out[n-1], false)
			if a == b {
				continue
			}
		}
		out = append(out, raw)
	}
	return out
}

func firstStream(pkts []models.Packet) []models.Packet {
	if len(pkts) == 0 {
		return nil
	}
	return ofStream(pkts, pkts[0].StreamID)
}

func deliveredStream(g mxGot, addr mxAddr) uint {
	for _, pkts := range [][]models.Packet{g.tg, g.user, g.data, g.target, g.ob} {
		if got := ofCall(pkts, uint(addr.c.src), uint(addr.c.dst)); len(got) > 0 {
			return got[0].StreamID
		}
	}
	return 0
}

func ipscOfCall(got []testutils.IPSCBurst, c call) []testutils.IPSCBurst {
	var out []testutils.IPSCBurst
	for _, b := range got {
		if b.Src == uint(c.src) && b.Dst == uint(c.dst) && b.GroupCall == c.group {
			out = append(out, b)
		}
	}
	return out
}

func packetsOf(raws []layer2.RawBurst, like []models.Packet) []models.Packet {
	out := make([]models.Packet, 0, len(raws))
	for i, raw := range raws {
		p := models.Packet{DMRData: raw}
		if i < len(like) {
			p.FrameType = like[i].FrameType
		}
		out = append(out, p)
	}
	return out
}

func assertSameDMRDPayloads(t *testing.T, want, got []models.Packet) {
	t.Helper()
	require.Len(t, got, len(want), "bursts")
	for i := range want {
		voice := want[i].FrameType == dmrconst.FrameVoice || want[i].FrameType == dmrconst.FrameVoiceSync
		wdt, wp := payloadOf(layer2.RawBurst(want[i].DMRData), voice)
		gdt, gp := payloadOf(layer2.RawBurst(got[i].DMRData), voice)
		require.Equal(t, wdt, gdt, "burst %d data type", i)
		if !voice && (wdt == enums.DataTypeVoiceLCHeader || wdt == enums.DataTypeTerminatorWithLC) {
			continue
		}
		require.Equal(t, wp, gp, "burst %d payload", i)
	}
}

// TestTransparencyNonVoiceDoesNotLink checks that only voice links a
// repeater to a dynamic talkgroup (decision D5): group data and CSBKs from a
// repeater follow the links it has and never make one.
func TestTransparencyNonVoiceDoesNotLink(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, mxRadio, "N0CALL")
		stack.SeedTalkgroup(t, 93, "TG93")
		stack.SeedTalkgroup(t, 94, "TG94")
		stack.SeedMMDVMRepeater(t, mxSrcRpt, mxRadio, testPassword)
		stack.SeedMMDVMRepeater(t, mxTGRpt, mxRadio, testPassword)
		stack.StartServers(t)
		a := testutils.NewMMDVMClient(mxSrcRpt, "", testPassword)
		b := testutils.NewMMDVMClient(mxTGRpt, "", testPassword)
		for _, c := range []*testutils.MMDVMClient{a, b} {
			defer c.Close()
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			require.NoError(t, c.WaitReady(handshakeWait))
		}
		time.Sleep(settleDuration)

		data := smsTransmission(t, &sms.Text{Format: sms.FormatIP, Source: mxRadio, Destination: 93, Group: true, Body: []byte("NO LINK")}, enums.DataTypeRate12)
		sendPackets(t, a.SendDMRD, data.packets(0xF001))
		var cs pdu.CSBK
		require.NoError(t, cs.SetPreamble(pdu.PreamblePDU{Group: true, TargetAddress: 93, SourceAddress: mxRadio}))
		cs.LastBlock = true
		sendPackets(t, a.SendDMRD, (&transmission{call: call{group: true, dst: 93, src: mxRadio}, bursts: []layer2.RawBurst{csbkBurst(t, &cs)}}).packets(0xF002))
		time.Sleep(drainWait)
		keyUp(t, a, mxRadio, 94, true, 0xF003)
		drainAll(500*time.Millisecond, a, b)

		require.NoError(t, b.SendDMRD(makeGroupVoicePacket(mxRadio, 93, 0xF004, false)))
		require.NoError(t, b.SendDMRD(makeGroupVoiceTermPacket(mxRadio, 93, 0xF004, false)))
		require.NoError(t, b.SendDMRD(makeGroupVoicePacket(mxRadio, 94, 0xF005, true)))
		require.NoError(t, b.SendDMRD(makeGroupVoiceTermPacket(mxRadio, 94, 0xF005, true)))
		got := a.Drain(drainWait)
		assert.Empty(t, ofCall(got, mxRadio, 93), "group data and a CSBK linked the repeater to talkgroup 93")
		assert.NotEmpty(t, ofCall(got, mxRadio, 94), "voice did not link the repeater to talkgroup 94")
	})
}

// TestTransparencyReplicas runs the hub as two replicas and checks that
// each transmission reaches each target once, from whichever replica the
// source and the target are on, and that only one transmission row is
// written for it.
func TestTransparencyReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, mxRadio, "N0CALL")
		stack.SeedTalkgroup(t, mxTG, "TG91")
		stack.SeedMMDVMRepeater(t, mxSrcRpt, mxRadio, testPassword)
		stack.SeedMMDVMRepeater(t, mxReplicaRpt, mxRadio, testPassword)
		stack.SeedIPSCRepeater(t, mxIPSC, mxRadio, ipscAuthKey)
		stack.AssignTS1StaticTG(t, mxReplicaRpt, mxTG)
		stack.AssignTS1StaticTG(t, mxIPSC, mxTG)
		stack.StartServers(t)
		replica := stack.SpawnFullReplica(t)

		ob := testutils.NewOpenBridgeClient(mxOBPeer, obPassword)
		defer ob.Close()
		require.NoError(t, ob.Connect(stack.OpenBridgeAddr))
		addr := ob.LocalAddr()
		stack.SeedPeer(t, mxOBPeer, mxRadio, obPassword, addr.IP.String(), addr.Port, false, true)
		stack.SeedPeerRule(t, mxOBPeer, false, 1, dmrconst.MaxDMRAddress)
		stack.SeedPeer(t, mxOBSrc, mxRadio, obPassword, "127.0.0.1", 0, true, false)
		stack.SeedPeerRule(t, mxOBSrc, true, 1, dmrconst.MaxDMRAddress)
		obSrc := testutils.NewOpenBridgeClient(mxOBSrc, obPassword)
		defer obSrc.Close()
		require.NoError(t, obSrc.Connect(stack.OpenBridgeAddr))

		src := testutils.NewMMDVMClient(mxSrcRpt, "", testPassword)
		defer src.Close()
		require.NoError(t, src.Connect(stack.MMDVMAddr))
		require.NoError(t, src.WaitReady(handshakeWait))
		target := testutils.NewMMDVMClient(mxReplicaRpt, "", testPassword)
		defer target.Close()
		require.NoError(t, target.Connect(replica.MMDVMAddr))
		require.NoError(t, target.WaitReady(handshakeWait))
		ipscTarget := testutils.NewIPSCClient(mxIPSC, ipscAuthKey)
		defer ipscTarget.Close()
		require.NoError(t, ipscTarget.Connect(replica.IPSCAddr))
		require.NoError(t, ipscTarget.WaitReady(handshakeWait))
		time.Sleep(settleDuration)

		c := call{group: true, dst: mxTG, src: mxRadio}
		stream := uint(0xF100)
		for _, p := range []mxPayload{voicePayload(), smsPayload("sms-ip", sms.FormatIP, "REPLICA TEST", enums.DataTypeRate12),
			csbkPayload("radio-check", "radio_check", 1, func(cs *pdu.CSBK, c call) error {
				return cs.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: c.src, CheckedAddress: c.dst})
			})} {
			for _, from := range []string{"mmdvm", "openbridge"} {
				stream++
				send := src.SendDMRD
				if from == "openbridge" {
					send = func(pk models.Packet) error {
						pk.Repeater = mxOBSrc
						return obSrc.SendRawPacket(pk)
					}
				}
				runReplicaCase(t, stack, from, p, c, stream, send, replicaTargets{target, ipscTarget, ob})
			}
		}
	})
}

type replicaTargets struct {
	mmdvm *testutils.MMDVMClient
	ipsc  *testutils.IPSCClient
	ob    *testutils.OpenBridgeClient
}

func runReplicaCase(t *testing.T, stack *testutils.IntegrationStack, from string, p mxPayload, c call, stream uint, send func(models.Packet) error, to replicaTargets) {
	t.Run(from+"/"+p.name, func(t *testing.T) {
		pkts := p.build(t, c)[0].packets(stream)
		sendPackets(t, send, pkts)
		ipscGot := make(chan []testutils.IPSCBurst, 1)
		go func() { ipscGot <- to.ipsc.Drain(time.Duration(len(pkts))*60*time.Millisecond + 3*time.Second) }()
		got := drainQuiet(time.Duration(len(pkts))*60*time.Millisecond+time.Second, to.mmdvm, to.ob)
		ipscBurstsGot := <-ipscGot
		route := needRoute()
		if p.voice {
			route = nil
		}
		t.Run("replica-target", func(t *testing.T) {
			needed := route
			if from == "openbridge" {
				needed = join(route, []string{"commit 0d"})
			}
			needs(t, needed...)
			assertSamePackets(t, pkts, ofStream(got[0], stream), mxReplicaRpt, false)
		})
		t.Run("replica-ipsc-target", func(t *testing.T) {
			needed := join(route, []string{"hub-a commit 5"})
			if from == "openbridge" {
				needed = join(needed, []string{"commit 0d"})
			}
			needs(t, needed...)
			raws := ipscBursts(t, ipscOfCall(ipscBurstsGot, c))
			assertSameDMRDPayloads(t, pkts, packetsOf(withoutRepeatedHeaders(raws), pkts))
		})
		if from == "mmdvm" {
			t.Run("openbridge-peer", func(t *testing.T) {
				needs(t, "commit 0d")
				assertSamePackets(t, pkts, ofStream(got[1], stream), mxOBPeer, false)
			})
		}
		t.Run("one-transmission-row", func(t *testing.T) {
			needed := needRows()
			if from == "openbridge" {
				needed = join(needed, []string{"commit 0d"})
			}
			needs(t, needed...)
			want := map[string]any{"kind": p.kind}
			assert.Equal(t, 1, countRows(t, stack.DB, "calls", "stream_id", stream, want))
			time.Sleep(time.Second)
			assert.Len(t, rowsOf(t, stack.DB, "calls", "stream_id", stream), 1)
		})
	})
}

func smsPacket(t *testing.T, text *sms.Text, rate enums.DataType) packet.Packet {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, rate)
	require.NoError(t, err)
	return p
}

func smsTransmission(t *testing.T, text *sms.Text, rate enums.DataType) transmission {
	t.Helper()
	p := smsPacket(t, text, rate)
	c := call{group: text.Group, dst: text.Destination, src: text.Source}
	data := packetBursts(t, &p)
	return transmission{call: c, bursts: append(preambles(t, c, 5, len(data)), data...)}
}

func responseTransmission(t *testing.T, p *packet.Packet, r packet.Response) transmission {
	t.Helper()
	h, ok := p.Header.AsConfirmed()
	require.True(t, ok)
	resp, err := packet.NewResponse(&h, r, nil)
	require.NoError(t, err)
	return transmission{call: call{dst: h.LLIDSource, src: h.LLIDDestination}, bursts: packetBursts(t, &resp)}
}

func ipscBursts(t *testing.T, got []testutils.IPSCBurst) []layer2.RawBurst {
	t.Helper()
	raws := make([]layer2.RawBurst, 0, len(got))
	for _, b := range got {
		u, err := ipscwire.DecodeUser(b.Data)
		if err != nil {
			continue
		}
		raw, err := u.RawBurst(0, layer2.SyncSourceBS)
		if err != nil {
			continue
		}
		raws = append(raws, raw)
	}
	return raws
}

func requireRows(t *testing.T, db *gorm.DB, table, col string, v any, want ...map[string]any) []map[string]any {
	t.Helper()
	rows := waitRows(t, db, table, col, v, want)
	if miss := unmatched(want, rows); len(miss) > 0 {
		t.Fatalf("%s where %s = %v: no row for %s; rows: %s", table, col, v, jsonText(miss), jsonText(project(rows, keysOf(want))))
	}
	return rows
}

func countRows(t *testing.T, db *gorm.DB, table, col string, v any, want map[string]any) int {
	t.Helper()
	rows := waitRows(t, db, table, col, v, []map[string]any{want})
	n := 0
	for _, r := range rows {
		if matches(want, r) {
			n++
		}
	}
	return n
}
