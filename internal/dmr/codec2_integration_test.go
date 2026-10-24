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
	"math/rand/v2"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	c2FID         = enums.FID(0xC2)
	c2Talker      = 1000001
	c2User        = 1000002
	c2TG1         = 91
	c2TG2         = 92
	c2KeyTG       = 99
	c2TalkerRpt   = 100001
	c2ListenRpt   = 100002
	c2UserRpt     = 100003
	c2LateRpt     = 100004
	c2IPSC        = 200001
	c2IPSCSrc     = 200002
	c2IPSCLate    = 200003
	c2OB          = 3129201
	c2OBEnhanced  = 3129202
	c2Parrot      = 9990
	c2Superframes = 3
)

func c2Options() layer3.ServiceOptions {
	return layer3.ServiceOptions{IsOpenVoiceCallMode: true, PriorityLevel: 2}
}

// c2LC returns the Voice LC of a Codec2 call as QRadioLink sends it: a
// group or unit to unit voice LC with the FID 0xC2, here with Service
// Options that are not zero, so that a rebuilt LC shows.
func c2LC(t *testing.T, c call) pdu.LCBytes {
	t.Helper()
	lc, err := pdu.NewVoiceLCBytes(c.group, c.dst, c.src, c2Options())
	require.NoError(t, err)
	lc.SetFID(c2FID)
	return lc
}

// c2Slots returns n vocoder slots in the framing of QRadioLink and dmrgo
// vocoder/codec2 at 2400 bit/s: 6 octets of Codec2 bits, then 3 zero
// octets, with no FEC. DMRHub builds without libcodec2, so the Codec2 bits
// are random rather than encoded speech; the hub does not decode them.
func c2Slots(seed uint64, n int) [][9]byte {
	r := rand.New(rand.NewPCG(seed, uint64(c2FID))) //nolint:gosec
	out := make([][9]byte, n)
	for i := range out {
		for j := range 6 {
			out[i][j] = byte(r.Uint32())
		}
	}
	return out
}

type c2Call struct {
	lc    pdu.LCBytes
	slots [][9]byte
	tx    transmission
}

// newC2Call builds a Codec2 call with dmrgo: the Voice LC Header, voice
// bursts with the embedded LC, and the Terminator with LC. A count of
// voice bursts that is not a whole number of superframes ends the call
// in the middle of a superframe, without padding.
func newC2Call(t *testing.T, c call, slot2 bool, bursts int, seed uint64) c2Call {
	t.Helper()
	lc := c2LC(t, c)
	vt, err := layer2.NewVoiceTransmitter(&layer2.VoiceTransmitterOptions{LC: pdu.DecodeFullLinkControlBytes(lc), ColorCode: 1, Source: layer2.SyncSourceMS})
	require.NoError(t, err)
	out := c2Call{lc: lc, slots: c2Slots(seed, bursts*layer2.VoiceSlotsPerBurst), tx: transmission{call: c, slot2: slot2}}
	add := func(raw layer2.RawBurst, frame dmrconst.FrameType, value uint) {
		out.tx.bursts = append(out.tx.bursts, raw)
		out.tx.frame = append(out.tx.frame, frame)
		out.tx.value = append(out.tx.value, value)
	}
	add(vt.Header(), dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader))
	voiceBursts, err := vt.AddSlots(out.slots...)
	require.NoError(t, err)
	require.Len(t, voiceBursts, bursts)
	for i, raw := range voiceBursts {
		frame := dmrconst.FrameVoice
		if i%6 == 0 {
			frame = dmrconst.FrameVoiceSync
		}
		add(raw, frame, uint(i%6))
	}
	add(vt.Terminator(), dmrconst.FrameDataSync, uint(enums.DataTypeTerminatorWithLC))
	requireC2LC(t, out.tx.bursts[0], enums.DataTypeVoiceLCHeader, lc)
	return out
}

func requireC2LC(t *testing.T, raw layer2.RawBurst, dt enums.DataType, want pdu.LCBytes) {
	t.Helper()
	cw, ok := raw.MMDVMHostPayload(dt)
	require.True(t, ok, "%s: the Full LC does not decode", dt)
	requireLCBytes(t, dt.String(), pdu.LCBytes(cw[:9]), want)
}

func requireLCBytes(t *testing.T, where string, got, want pdu.LCBytes) {
	t.Helper()
	so, _ := got.ServiceOptions()
	wso, _ := want.ServiceOptions()
	require.Equal(t, want.FID(), got.FID(), "%s: FID", where)
	require.Equal(t, wso, so, "%s: Service Options", where)
	require.Equal(t, hex.EncodeToString(want[:]), hex.EncodeToString(got[:]), "%s: LC", where)
}

// requireEmbeddedLC checks that the embedded LC of each whole superframe
// of the voice bursts pkts is want.
func requireEmbeddedLC(t *testing.T, pkts []models.Packet, want pdu.LCBytes) {
	t.Helper()
	n := 0
	for i := 0; i+3 < len(pkts); i++ {
		if !isVoice(pkts[i]) || pkts[i].DTypeOrVSeq != 1 {
			continue
		}
		var f [4][4]byte
		for k := range 4 {
			require.Equal(t, uint(k+1), pkts[i+k].DTypeOrVSeq, "packet %d position", i+k)
			f[k] = layer2.RawBurst(pkts[i+k].DMRData).EmbeddedData()
		}
		lc, res := layer2.DecodeEmbeddedLC(f)
		require.False(t, res.Uncorrectable, "embedded LC at packet %d", i)
		requireLCBytes(t, "embedded LC", lc, want)
		n++
	}
	require.NotZero(t, n, "no superframe with an embedded LC")
}

func isVoice(p models.Packet) bool {
	return p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync
}

// checkC2 checks, bit for bit, the Codec2 call got that a receiver got
// against want: the same frames, the LC with its FID and Service Options
// in the Voice LC Header and Terminator with LC, the 216 voice bits, EMB
// and embedded signalling of each voice burst, and the embedded LC.
func checkC2(t *testing.T, want []models.Packet, lc pdu.LCBytes, got []models.Packet) {
	t.Helper()
	require.Len(t, got, len(want), "packets")
	for i := range want {
		w, g := layer2.RawBurst(want[i].DMRData), layer2.RawBurst(got[i].DMRData)
		require.Equal(t, want[i].FrameType, got[i].FrameType, "packet %d frame type", i)
		require.Equal(t, want[i].DTypeOrVSeq, got[i].DTypeOrVSeq, "packet %d data type or position", i)
		if !isVoice(want[i]) {
			requireC2LC(t, g, enums.DataType(want[i].DTypeOrVSeq), lc)
			continue
		}
		require.Equal(t, hex.EncodeToString(flatSlots(w)), hex.EncodeToString(flatSlots(g)), "packet %d voice bits", i)
		if want[i].DTypeOrVSeq != 0 {
			require.Equal(t, w.EMB(), g.EMB(), "packet %d EMB", i)
			require.Equal(t, w.EmbeddedData(), g.EmbeddedData(), "packet %d embedded signalling", i)
		}
	}
	requireEmbeddedLC(t, got, lc)
}

func flatSlots(raw layer2.RawBurst) []byte {
	s := raw.VoiceSlots()
	return append(append(s[0][:], s[1][:]...), s[2][:]...)
}

// ambeRebuilt returns raw as an IPSC hop leaves it: the three slots
// decoded as AMBE+2 frames, which drops their FEC, and coded again.
func ambeRebuilt(t *testing.T, raw layer2.RawBurst) layer2.RawBurst {
	t.Helper()
	frames, _ := raw.AMBEFrames()
	out := raw
	require.NoError(t, out.SetAMBEFrames(frames))
	return out
}

type c2Net struct {
	stack                *testutils.IntegrationStack
	talker, listen, user *testutils.MMDVMClient
	ob, obEnhanced       *testutils.OpenBridgeClient
	ipsc, ipscSrc        *testutils.IPSCClient
	mu                   sync.Mutex
	stream               uint
}

func newC2Net(t *testing.T, stack *testutils.IntegrationStack) *c2Net {
	t.Helper()
	stack.SeedUser(t, c2Talker, "N0CALL")
	stack.SeedUser(t, c2User, "N1CALL")
	for _, tg := range []uint{c2TG1, c2TG2, c2KeyTG} {
		stack.SeedTalkgroup(t, tg, "TG")
	}
	for id, owner := range map[uint]uint{c2TalkerRpt: c2Talker, c2ListenRpt: c2Talker, c2UserRpt: c2User, c2LateRpt: c2Talker} {
		stack.SeedMMDVMRepeater(t, id, owner, testPassword)
	}
	for _, id := range []uint{c2IPSC, c2IPSCSrc, c2IPSCLate} {
		stack.SeedIPSCRepeater(t, id, c2Talker, ipscAuthKey)
	}
	stack.AssignTS1StaticTG(t, c2ListenRpt, c2TG1)
	stack.AssignTS2StaticTG(t, c2ListenRpt, c2TG2)
	stack.AssignTS1StaticTG(t, c2LateRpt, c2TG1)
	stack.AssignTS1StaticTG(t, c2IPSC, c2TG1)
	stack.AssignTS1StaticTG(t, c2IPSCLate, c2TG1)
	stack.StartServers(t)

	n := &c2Net{stack: stack, stream: 0xC20000}
	n.talker, n.listen, n.user = c2MMDVM(t, stack, c2TalkerRpt), c2MMDVM(t, stack, c2ListenRpt), c2MMDVM(t, stack, c2UserRpt)
	n.ipsc, n.ipscSrc = c2IPSCPeer(t, stack, c2IPSC), c2IPSCPeer(t, stack, c2IPSCSrc)
	n.ob = c2OBPeer(t, stack, c2OB, false)
	n.obEnhanced = c2OBPeer(t, stack, c2OBEnhanced, true)
	time.Sleep(settleDuration)
	n.drain(500 * time.Millisecond)
	return n
}

func c2MMDVM(t *testing.T, stack *testutils.IntegrationStack, id uint) *testutils.MMDVMClient {
	t.Helper()
	c := testutils.NewMMDVMClient(uint32(id), "", testPassword) //nolint:gosec
	t.Cleanup(c.Close)
	require.NoError(t, c.Connect(stack.MMDVMAddr))
	require.NoError(t, c.WaitReady(handshakeWait))
	return c
}

func c2IPSCPeer(t *testing.T, stack *testutils.IntegrationStack, id uint32) *testutils.IPSCClient {
	t.Helper()
	c := testutils.NewIPSCClient(id, ipscAuthKey)
	t.Cleanup(c.Close)
	require.NoError(t, c.Connect(stack.IPSCAddr))
	require.NoError(t, c.WaitReady(handshakeWait))
	return c
}

func c2OBPeer(t *testing.T, stack *testutils.IntegrationStack, id uint32, enhanced bool) *testutils.OpenBridgeClient {
	t.Helper()
	c := testutils.NewOpenBridgeClient(id, obPassword)
	t.Cleanup(c.Close)
	require.NoError(t, c.Connect(stack.OpenBridgeAddr))
	addr := c.LocalAddr()
	stack.SeedPeer(t, uint(id), c2Talker, obPassword, addr.IP.String(), addr.Port, true, true)
	if enhanced {
		require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", id).Update("enhanced", true).Error)
	}
	stack.SeedPeerRule(t, uint(id), true, 1, dmrconst.MaxDMRAddress)
	stack.SeedPeerRule(t, uint(id), false, 1, dmrconst.MaxDMRAddress)
	return c
}

func (n *c2Net) nextStream() uint {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stream += 16
	return n.stream
}

type c2Got struct {
	listen, user, talker, ob, obEnhanced []models.Packet
	ipsc                                 []testutils.IPSCBurst
}

type enhancedDrainer struct{ c *testutils.OpenBridgeClient }

func (e enhancedDrainer) Drain(d time.Duration) []models.Packet {
	var out []models.Packet
	for _, b := range e.c.DrainBridges(d) {
		if b.Version >= 4 {
			out = append(out, models.PacketFromData(&b.Data, false))
		}
	}
	return out
}

func (n *c2Net) drain(d time.Duration) c2Got {
	var g c2Got
	done := make(chan []testutils.IPSCBurst, 1)
	stop := time.Now().Add(d)
	go func() {
		var all []testutils.IPSCBurst
		for {
			got := n.ipsc.Drain(500 * time.Millisecond)
			all = append(all, got...)
			if len(got) == 0 && time.Now().After(stop) {
				done <- all
				return
			}
		}
	}()
	got := drainQuiet(d, n.listen, n.user, n.talker, n.ob, enhancedDrainer{n.obEnhanced})
	g.listen, g.user, g.talker, g.ob, g.obEnhanced = got[0], got[1], got[2], got[3], got[4]
	g.ipsc = <-done
	return g
}

func sendC2(t *testing.T, send func(models.Packet) error, pkts []models.Packet) {
	t.Helper()
	for _, p := range pkts {
		require.NoError(t, send(p))
		time.Sleep(30 * time.Millisecond)
	}
}

func callWait(pkts []models.Packet) time.Duration {
	return time.Duration(len(pkts))*60*time.Millisecond + time.Second
}

// ipscC2 decodes the IPSC packets of the call c, without the copies of the
// Voice LC Header that IPSC repeats.
func ipscC2(t *testing.T, got []testutils.IPSCBurst, c call) []ipscwire.User {
	t.Helper()
	var out []ipscwire.User
	for _, b := range ipscOfCall(got, c) {
		u, err := ipscwire.DecodeUser(b.Data)
		require.NoError(t, err)
		if dt, ok := u.Burst.DataType(); ok && dt == enums.DataTypeVoiceLCHeader && len(out) > 0 {
			if pdt, ok := out[len(out)-1].Burst.DataType(); ok && pdt == enums.DataTypeVoiceLCHeader {
				continue
			}
		}
		out = append(out, u)
	}
	return out
}

// checkC2IPSC checks the Codec2 call that an IPSC peer got. The LC must be
// the sent one, with its FID and Service Options, in the Voice LC Header,
// the Terminator with LC and each burst E, and the EMB and embedded
// signalling of bursts B to F must be unchanged. IPSC carries each voice
// burst as three 49-bit AMBE+2 frames without FEC, so the voice must be
// the AMBE+2 decoding of the sent burst, which is all the IPSC wire can
// carry of 216 Codec2 bits. A burst E before packet lcFrom, which a peer
// that joined late gets before a whole embedded LC, is not checked for the
// LC.
func checkC2IPSC(t *testing.T, want []models.Packet, lc pdu.LCBytes, got []ipscwire.User, lcFrom int) {
	t.Helper()
	require.Len(t, got, len(want), "IPSC packets")
	for i := range want {
		u, w := got[i], layer2.RawBurst(want[i].DMRData)
		if !isVoice(want[i]) {
			dt := enums.DataType(want[i].DTypeOrVSeq)
			gdt, ok := u.Burst.DataType()
			require.True(t, ok && gdt == dt, "packet %d: burst %v, want %s", i, u.Burst, dt)
			require.EqualValues(t, 96, u.Data.Bits, "packet %d LC bits", i)
			requireLCBytes(t, dt.String(), pdu.LCBytes(u.Data.Octets[:9]), lc)
			continue
		}
		require.True(t, u.IsVoice(), "packet %d is not voice", i)
		ambe, _, _ := w.AMBEVoice()
		require.Equal(t, hex.EncodeToString(ambe[:]), hex.EncodeToString(u.Voice.AMBE[:]), "packet %d AMBE payload", i)
		switch want[i].DTypeOrVSeq {
		case 0:
			require.Equal(t, ipscwire.VoiceLayoutA, u.Voice.Layout, "packet %d layout", i)
			continue
		case 4:
			require.Equal(t, ipscwire.VoiceLayoutE, u.Voice.Layout, "packet %d layout", i)
			if i >= lcFrom {
				requireLCBytes(t, "burst E LC", pdu.LCBytes(u.Voice.LC), lc)
			}
		}
		emb := w.EMB()
		info, err := emb.Info()
		require.NoError(t, err)
		require.Equal(t, info, u.Voice.EMB, "packet %d EMB", i)
		require.Equal(t, w.EmbeddedData(), u.Voice.Embedded, "packet %d embedded signalling", i)
	}
}

func (n *c2Net) checkRow(t *testing.T, stream uint, src uint32) {
	t.Helper()
	t.Run("last-heard", func(t *testing.T) {
		assert.Equal(t, 1, countRows(t, n.stack.DB, "calls", "stream_id", stream, map[string]any{"kind": "voice", "source_id": src}), "voice rows")
		for _, r := range rowsOf(t, n.stack.DB, "calls", "stream_id", stream) {
			t.Logf("last heard row: kind %v sub_kind %q fid %v", r["kind"], text(r["sub_kind"]), r["fid"])
		}
	})
}

// TestCodec2Transparency sends a Codec2 call (FID 0xC2 in the Voice LC,
// Codec2 bits in the voice slots without AMBE FEC, as QRadioLink and dmrgo
// vocoder/codec2 send it) over every path of the hub. Every receiver must
// get the LC with its FID and Service Options and the 216 voice bits of
// each burst unchanged; on IPSC, which carries AMBE+2 frames, the LC must
// be unchanged and the voice as the IPSC wire defines it.
func TestCodec2Transparency(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newC2Net(t, stack)
		for _, tc := range []c2Case{
			{"group-ts1", call{group: true, dst: c2TG1, src: c2Talker}, false},
			{"group-ts2", call{group: true, dst: c2TG2, src: c2Talker}, true},
			{"private-ts1", call{dst: c2User, src: c2Talker}, false},
			{"private-ts2", call{dst: c2User, src: c2Talker}, true},
		} {
			runC2FromMMDVM(t, n, tc)
		}
		runC2FromOB(t, n, "openbridge-v1-to-mmdvm", func(p models.Packet) error {
			p.Repeater = c2OB
			return n.ob.SendRawPacket(p)
		})
		runC2FromOB(t, n, "openbridge-enhanced-to-mmdvm", func(p models.Packet) error {
			p.Repeater = c2OBEnhanced
			return n.obEnhanced.SendEnhancedPacket(p, 5)
		})
		runC2FromIPSC(t, n)
	})
}

type c2Case struct {
	name  string
	c     call
	slot2 bool
}

func runC2FromMMDVM(t *testing.T, n *c2Net, tc c2Case) {
	t.Run("mmdvm-to-mmdvm/"+tc.name, func(t *testing.T) {
		if !tc.c.group {
			keyUp(t, n.user, c2User, c2KeyTG, tc.slot2, n.nextStream())
			n.drain(300 * time.Millisecond)
		}
		stream := n.nextStream()
		cc := newC2Call(t, tc.c, tc.slot2, 6*c2Superframes, uint64(stream))
		sent := cc.tx.packets(stream)
		sendC2(t, n.talker.SendDMRD, sent)
		g := n.drain(callWait(sent))
		to, rpt := g.listen, uint(c2ListenRpt)
		if !tc.c.group {
			to, rpt = g.user, c2UserRpt
		}
		got := ofStream(ofCall(to, uint(tc.c.src), uint(tc.c.dst)), stream)
		checkC2(t, sent, cc.lc, got)
		assertSamePackets(t, sent, got, rpt, tc.slot2)
		if tc.name == "group-ts1" {
			checkC2Exact(t, "openbridge-v1", sent, cc.lc, ofStream(g.ob, stream), c2OB)
			checkC2Exact(t, "openbridge-enhanced", sent, cc.lc, ofStream(g.obEnhanced, stream), c2OBEnhanced)
			checkC2OverIPSC(t, sent, cc.lc, g.ipsc, tc.c)
		}
		n.checkRow(t, stream, tc.c.src)
	})
}

func checkC2Exact(t *testing.T, name string, sent []models.Packet, lc pdu.LCBytes, got []models.Packet, repeater uint) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		checkC2(t, sent, lc, got)
		assertSamePackets(t, sent, got, repeater, false)
	})
}

func checkC2OverIPSC(t *testing.T, sent []models.Packet, lc pdu.LCBytes, got []testutils.IPSCBurst, c call) {
	t.Helper()
	t.Run("ipsc", func(t *testing.T) {
		checkC2IPSC(t, sent, lc, ipscC2(t, got, c), 0)
	})
}

func runC2FromOB(t *testing.T, n *c2Net, name string, send func(models.Packet) error) {
	c := call{group: true, dst: c2TG1, src: c2Talker}
	t.Run(name, func(t *testing.T) {
		stream := n.nextStream()
		cc := newC2Call(t, c, false, 6*c2Superframes, uint64(stream))
		sent := cc.tx.packets(stream)
		sendC2(t, send, sent)
		g := n.drain(callWait(sent))
		got := ofStream(g.listen, stream)
		checkC2(t, sent, cc.lc, got)
		assertSamePackets(t, sent, got, c2ListenRpt, false)
		checkC2OverIPSC(t, sent, cc.lc, g.ipsc, c)
		n.checkRow(t, stream, c.src)
	})
}

// runC2FromIPSC sends the call from an IPSC peer. The MMDVM and
// OpenBridge receivers must get the LC unchanged and the voice as the
// IPSC wire carried it, the AMBE+2 frames of each burst coded again.
func runC2FromIPSC(t *testing.T, n *c2Net) {
	c := call{group: true, dst: c2TG1, src: c2Talker}
	t.Run("ipsc-to-mmdvm", func(t *testing.T) {
		stream := n.nextStream()
		cc := newC2Call(t, c, false, 6*c2Superframes, uint64(stream))
		sent := cc.tx.packets(stream)
		want := make([]models.Packet, len(sent))
		for i, p := range sent {
			want[i] = p
			h := layer2.Hint{Kind: layer2.KindData, DataType: enums.DataType(p.DTypeOrVSeq), HasDataType: true}
			if isVoice(p) {
				pos, _ := enums.VoiceBurstAt(int(p.DTypeOrVSeq))
				h = layer2.Hint{Kind: layer2.KindVoice, VoiceBurst: pos}
				want[i].DMRData = ambeRebuilt(t, layer2.RawBurst(p.DMRData))
			}
			require.NoError(t, n.ipscSrc.SendBurst(p.Src, p.Dst, p.GroupCall, p.Slot, uint32(stream), layer2.RawBurst(p.DMRData), h, i == len(sent)-1)) //nolint:gosec
			time.Sleep(30 * time.Millisecond)
		}
		g := n.drain(callWait(sent))
		got := firstStream(ofCall(g.listen, uint(c.src), uint(c.dst)))
		checkC2(t, want, cc.lc, got)
		checkC2Payloads(t, "openbridge-v1", want, cc.lc, firstStream(ofCall(g.ob, uint(c.src), uint(c.dst))))
		if len(got) > 0 {
			n.checkRow(t, got[0].StreamID, c.src)
		}
	})
}

func checkC2Payloads(t *testing.T, name string, want []models.Packet, lc pdu.LCBytes, got []models.Packet) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		checkC2(t, want, lc, got)
	})
}

// TestCodec2LateEntry sends a Codec2 call and connects a hotspot and an
// IPSC peer of the talkgroup in its second superframe, after burst C. Each
// must get the rest of the call unchanged, and a receiver that joins late
// must learn the call, with the FID 0xC2, from the embedded LC. The IPSC
// peer must get the LC of the call in burst E from the first whole
// superframe on.
func TestCodec2LateEntry(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newC2Net(t, stack)
		c := call{group: true, dst: c2TG1, src: c2Talker}
		stream := n.nextStream()
		cc := newC2Call(t, c, false, 6*c2Superframes, uint64(stream))
		sent := cc.tx.packets(stream)
		const join = 1 + 6 + 3

		sendC2(t, n.talker.SendDMRD, sent[:join])
		late := c2MMDVM(t, stack, c2LateRpt)
		lateIPSC := c2IPSCPeer(t, stack, c2IPSCLate)
		time.Sleep(settleDuration)
		late.Drain(100 * time.Millisecond)
		lateIPSC.Drain(100 * time.Millisecond)
		sendC2(t, n.talker.SendDMRD, sent[join:])
		var gotIPSC []testutils.IPSCBurst
		var wg sync.WaitGroup
		wg.Add(1)
		go func() { defer wg.Done(); gotIPSC = lateIPSC.Drain(callWait(sent)) }()
		got := ofStream(drainQuiet(callWait(sent), late)[0], stream)
		wg.Wait()
		n.drain(100 * time.Millisecond)

		checkLateMMDVM(t, sent[join:], cc.lc, got)
		checkLateIPSC(t, sent[join:], cc.lc, ipscC2(t, gotIPSC, c), 6-(join-1)%6)
	})
}

func checkLateMMDVM(t *testing.T, sent []models.Packet, lc pdu.LCBytes, got []models.Packet) {
	t.Helper()
	t.Run("mmdvm", func(t *testing.T) {
		assertSamePackets(t, sent, got, c2LateRpt, false)
		checkC2(t, sent, lc, got)
		rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
		require.NoError(t, err)
		events := make([]voice.Event, 0, voice.MaxEvents)
		var start *voice.Call
		for i := range got {
			d := got[i].Data()
			raw := d.Burst
			now := time.Duration(i) * 60 * time.Millisecond
			for _, e := range rx.Burst(now, &raw, d.Hint(now), events[:0]) {
				if e.Kind == voice.EventCallStart && start == nil {
					call := e.Call
					start = &call
				}
			}
		}
		require.NotNil(t, start, "the late receiver found no call")
		assert.Equal(t, voice.OriginEmbedded, start.Origin)
		assert.Equal(t, c2FID, start.FID)
		assert.Equal(t, c2Options(), start.Options)
	})
}

func checkLateIPSC(t *testing.T, sent []models.Packet, lc pdu.LCBytes, got []ipscwire.User, lcFrom int) {
	t.Helper()
	t.Run("ipsc", func(t *testing.T) {
		checkC2IPSC(t, sent, lc, got, lcFrom)
	})
}

// TestCodec2HandoffResume starts a Codec2 call, restarts the MMDVM server
// as in a handoff in the middle of it, and finishes the call. The
// listening hotspot must get the whole call unchanged.
func TestCodec2HandoffResume(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newC2Net(t, stack)
		c := call{group: true, dst: c2TG1, src: c2Talker}
		stream := n.nextStream()
		cc := newC2Call(t, c, false, 6*c2Superframes, uint64(stream))
		sent := cc.tx.packets(stream)
		const half = 1 + 6 + 3

		sendC2(t, n.talker.SendDMRD, sent[:half])
		stack.RestartMMDVM(t, testutils.RestartHandoff)
		sendC2(t, n.talker.SendDMRD, sent[half:])
		g := n.drain(callWait(sent))
		got := ofStream(g.listen, stream)
		checkC2(t, sent, cc.lc, got)
		assertSamePackets(t, sent, got, c2ListenRpt, false)
		n.checkRow(t, stream, c.src)
	})
}

// TestCodec2Parrot calls the parrot with a Codec2 call that ends in the
// middle of a superframe. The reply must carry the LC of the caller, with
// its FID and Service Options and the addresses of the reply, and the
// recorded voice unchanged. The last superframe must be padded with the
// last recorded slot, not with AMBE+2 silence, which is noise to Codec2.
func TestCodec2Parrot(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newC2Net(t, stack)
		runParrot(t, stack)
		c := call{dst: c2Parrot, src: c2Talker}
		stream := n.nextStream()
		const bursts = 6*2 + 3
		cc := newC2Call(t, c, false, bursts, uint64(stream))
		sent := cc.tx.packets(stream)
		sendC2(t, n.talker.SendDMRD, sent)

		var reply []models.Packet
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			got := ofCall(n.talker.Drain(time.Second), c2Parrot, c2Talker)
			reply = append(reply, got...)
			if len(reply) > 0 && len(got) == 0 {
				break
			}
		}
		require.Len(t, reply, 1+6*3+1, "reply packets")

		want := cc.lc
		require.NoError(t, want.SetCall(false, c2Talker, c2Parrot))
		requireC2LC(t, layer2.RawBurst(reply[0].DMRData), enums.DataTypeVoiceLCHeader, want)
		requireC2LC(t, layer2.RawBurst(reply[len(reply)-1].DMRData), enums.DataTypeTerminatorWithLC, want)
		requireEmbeddedLC(t, reply[1:len(reply)-1], want)
		last := cc.slots[len(cc.slots)-1]
		for i, p := range reply[1 : len(reply)-1] {
			require.True(t, isVoice(p), "packet %d is not voice", i+1)
			require.Equal(t, uint(i%6), p.DTypeOrVSeq, "packet %d position", i+1)
			slots := layer2.RawBurst(p.DMRData).VoiceSlots()
			for k, s := range slots {
				j := i*layer2.VoiceSlotsPerBurst + k
				exp := last
				if j < len(cc.slots) {
					exp = cc.slots[j]
				}
				require.Equal(t, hex.EncodeToString(exp[:]), hex.EncodeToString(s[:]), "slot %d", j)
			}
		}
	})
}
