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
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/codec2"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// tcNet is a network with a Codec2 hotspot (tcC2Rpt, AMBE until it is
// heard), an AMBE hotspot that listens on tcTG, a hotspot whose TS1 an
// admin set to Codec2 and that listens on tcTG, an AMBE hotspot that
// talks, an IPSC repeater on tcTG, and two OpenBridge peers, one AMBE
// only and one that carries Codec2. The hub converts with a fake
// AMBEserver.
type tcNet struct {
	stack                              *testutils.IntegrationStack
	ambe                               *testutils.FakeAMBEServer
	c2, ambeListen, c2Listen, ambeTalk *testutils.MMDVMClient
	ipsc                               *testutils.IPSCClient
	ob, obCodec2                       *testutils.OpenBridgeClient
	mu                                 sync.Mutex
	stream                             uint
}

func newTCNet(t *testing.T, stack *testutils.IntegrationStack) *tcNet {
	t.Helper()
	stack.SeedUser(t, tcTalker, "N0CALL")
	stack.SeedUser(t, tcOther, "N1CALL")
	stack.SeedTalkgroup(t, tcTG, "TG")
	for _, id := range []uint{tcC2Rpt, tcAMBERpt, tcC2Listen} {
		stack.SeedMMDVMRepeater(t, id, tcTalker, testPassword)
	}
	stack.SeedMMDVMRepeater(t, tcAMBETalkRpt, tcOther, testPassword)
	stack.SeedIPSCRepeater(t, tcIPSC, tcTalker, ipscAuthKey)
	for _, id := range []uint{tcAMBERpt, tcC2Listen, tcIPSC} {
		stack.AssignTS1StaticTG(t, id, tcTG)
	}
	require.NoError(t, models.SetRepeaterCodec(stack.DB, tcC2Listen, models.CodecCodec2, models.CodecSettingAuto))
	stack.StartServers(t)
	n := &tcNet{stack: stack, ambe: testutils.StartFakeAMBEServer(t), stream: 0xA0000}
	roomy(stack.Hub)
	stack.Hub.SetAMBEServers(ambeAt(n.ambe.Addr()))
	n.c2, n.ambeListen, n.c2Listen = c2MMDVM(t, stack, tcC2Rpt), c2MMDVM(t, stack, tcAMBERpt), c2MMDVM(t, stack, tcC2Listen)
	n.ambeTalk = c2MMDVM(t, stack, tcAMBETalkRpt)
	n.ipsc = c2IPSCPeer(t, stack, tcIPSC)
	n.ob = c2OBPeer(t, stack, tcOB, false)
	n.obCodec2 = c2OBPeer(t, stack, tcOBCodec2, false)
	require.NoError(t, stack.DB.Model(&models.Peer{}).Where("id = ?", tcOBCodec2).Update("codec2", true).Error)
	time.Sleep(settleDuration)
	n.drain(500 * time.Millisecond)
	return n
}

func (n *tcNet) nextStream() uint {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.stream += 16
	return n.stream
}

type tcGot struct {
	c2, ambeListen, c2Listen, ambeTalk, ob, obCodec2 []models.Packet
	ipsc                                             []testutils.IPSCBurst
}

func (n *tcNet) drain(d time.Duration) tcGot {
	var g tcGot
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
	got := drainQuiet(d, n.c2, n.ambeListen, n.c2Listen, n.ambeTalk, n.ob, n.obCodec2)
	g.c2, g.ambeListen, g.c2Listen, g.ambeTalk, g.ob, g.obCodec2 = got[0], got[1], got[2], got[3], got[4], got[5]
	g.ipsc = <-done
	return g
}

// requireCopy checks that got is the call sent converted: the same
// packets, with the LC lc in the Voice LC Header, the Terminator with LC
// and the embedded LC, the EMB and sync of each voice burst kept, and the
// voice slots want, or for a nil want voice slots other than those sent.
func requireCopy(t *testing.T, sent, got []models.Packet, repeater uint, lc pdu.LCBytes, want [][9]byte) {
	t.Helper()
	require.Len(t, got, len(sent), "packets")
	k := 0
	for i := range sent {
		s, g := sent[i], got[i]
		require.Equal(t, s.FrameType, g.FrameType, "packet %d frame type", i)
		require.Equal(t, s.DTypeOrVSeq, g.DTypeOrVSeq, "packet %d position", i)
		require.Equal(t, s.Src, g.Src, "packet %d src", i)
		require.Equal(t, s.Dst, g.Dst, "packet %d dst", i)
		require.Equal(t, s.StreamID, g.StreamID, "packet %d stream", i)
		require.Equal(t, repeater, g.Repeater, "packet %d repeater", i)
		if !isVoice(s) {
			requireC2LC(t, layer2.RawBurst(g.DMRData), enums.DataType(s.DTypeOrVSeq), lc)
			continue
		}
		raw, orig := layer2.RawBurst(g.DMRData), layer2.RawBurst(s.DMRData)
		if s.DTypeOrVSeq == 0 {
			gs, _ := raw.Sync()
			os, _ := orig.Sync()
			require.Equal(t, os, gs, "packet %d sync", i)
		} else {
			require.Equal(t, orig.EMB(), raw.EMB(), "packet %d EMB", i)
		}
		origSlots := orig.VoiceSlots()
		for j, slot := range raw.VoiceSlots() {
			if want == nil {
				require.NotEqual(t, origSlots[j], slot, "packet %d frame %d was not converted", i, j)
				continue
			}
			require.Equal(t, hex.EncodeToString(want[k][:]), hex.EncodeToString(slot[:]), "packet %d frame %d", i, j)
			k++
		}
	}
	requireEmbeddedLC(t, got, lc)
}

// ambeCopySlots returns the AMBE slots that the fake AMBEserver made of
// the audio it encoded from index from on, and checks that the audio is
// the Codec2 decoding of slots. Only the WebAssembly backend decodes the
// same samples every time; the native libcodec2 shares one random number
// generator between its decoders, so it is checked for audio only.
func ambeCopySlots(t *testing.T, srv *testutils.FakeAMBEServer, from int, slots [][9]byte) [][9]byte {
	t.Helper()
	pcm := srv.EncodedPCM()
	require.Len(t, pcm, from+len(slots), "the AMBE server encoded each frame of the call once")
	pcm = pcm[from:]
	out := make([][9]byte, len(pcm))
	for i := range pcm {
		out[i] = testutils.FakeAMBEEncode(pcm[i][:])
	}
	if codec2.Backend != "wasm" {
		return out
	}
	dec, err := codec2.New(nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = dec.Close() })
	for i, s := range slots {
		want, _, err := dec.Decode(nil, s)
		require.NoError(t, err)
		require.Equal(t, want, pcm[i][:], "frame %d is not the decoding of the call", i)
	}
	return out
}

// codec2CopySlots returns the Codec2 frames that the hub makes, in mode,
// of the AMBE slots, from what the fake AMBEserver decodes them to. The
// native libcodec2 keeps state that its encoders share, so with it the
// frames cannot be told in advance and codec2CopySlots returns nil.
func codec2CopySlots(t *testing.T, mode codec2.Mode, slots [][9]byte) [][9]byte {
	t.Helper()
	if codec2.Backend != "wasm" {
		return nil
	}
	enc, err := codec2.New(&codec2.Config{Mode: mode})
	require.NoError(t, err)
	t.Cleanup(func() { _ = enc.Close() })
	out := make([][9]byte, len(slots))
	for i, s := range slots {
		pcm := testutils.FakeAMBEDecode(s)
		out[i], err = enc.Encode(pcm[:])
		require.NoError(t, err)
	}
	return out
}

func ambeAt(addrs ...string) []transcode.ServerConfig {
	out := make([]transcode.ServerConfig, 0, len(addrs))
	for _, a := range addrs {
		out = append(out, transcode.ServerConfig{Addr: a})
	}
	return out
}

func missedSeconds() float64 {
	var sum float64
	for _, reason := range []string{"timeout", "late", "unreachable", "error"} {
		sum += testutil.ToFloat64(metrics.NewMetrics().TranscodeMissedSecondsTotal.WithLabelValues(models.CodecAMBE, reason))
	}
	return sum
}

func transcodeFrames(from, frame, output string) float64 {
	return testutil.ToFloat64(metrics.NewMetrics().TranscodeFramesTotal.WithLabelValues(from, frame, output))
}

func (n *tcNet) requireRow(t *testing.T, stream uint, want tcRow) {
	t.Helper()
	requireCodecRow(t, n.stack, stream, want)
}

func (n *tcNet) heard(t *testing.T, id uint) models.Repeater {
	t.Helper()
	var r models.Repeater
	require.NoError(t, n.stack.DB.First(&r, id).Error)
	return r
}

func (n *tcNet) send(t *testing.T, c *testutils.MMDVMClient, cc c2Call, stream uint) ([]models.Packet, tcGot) {
	t.Helper()
	sent := cc.tx.packets(stream)
	sendC2(t, c.SendDMRD, sent)
	return sent, n.drain(callWait(sent))
}

// TestTranscoding sends calls between a Codec2 hotspot and AMBE targets.
// A Codec2 call reaches every AMBE target, IPSC and the AMBE-only peer
// converted once, and the Codec2 targets unchanged; an AMBE call reaches
// the Codec2 timeslots converted once, in the Codec2 mode last heard on
// them. A slot is Codec2 only after a call with a Codec2 header, or one
// whose header is lost with a Codec2 embedded LC, never from its voice
// bits, and an AMBE call clears it. When the AMBE server stops answering,
// the audio goes out as silence and the call row counts it.
func TestTranscoding(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		group := call{group: true, dst: tcTG, src: tcTalker}
		lc0 := tcLC(t, group, enums.FIDStandardized)

		step(t, "codec2-call-to-ambe-targets", func(t *testing.T) {
			before := len(n.ambe.EncodedPCM())
			decoded := transcodeFrames(models.CodecCodec2, "good", "voice")
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent, g := n.send(t, n.c2, cc, stream)
			want := ambeCopySlots(t, n.ambe, before, cc.slots)

			got := ofStream(g.ambeListen, stream)
			requireCopy(t, sent, got, tcAMBERpt, lc0, want)
			checkC2IPSC(t, got, lc0, ipscC2(t, g.ipsc, group), 0)
			obGot := ofStream(g.ob, stream)
			require.Len(t, obGot, len(got), "AMBE-only peer")
			for i := range got {
				require.Equal(t, got[i].DMRData, obGot[i].DMRData, "AMBE-only peer packet %d is not the shared copy", i)
			}
			assertSamePackets(t, sent, ofStream(g.c2Listen, stream), tcC2Listen, false)
			assertSamePackets(t, sent, ofStream(g.obCodec2, stream), tcOBCodec2, false)
			assert.Empty(t, n.ambe.Errors())
			assert.GreaterOrEqual(t, transcodeFrames(models.CodecCodec2, "good", "voice")-decoded, float64(tcFrames), "the reports of the Codec2 decoder reach the metrics")

			n.requireRow(t, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400), sentAMBE: 3})
			r := n.heard(t, tcC2Rpt)
			assert.Equal(t, models.CodecCodec2, r.TS1HeardCodec)
			assert.Equal(t, int(codec2.Mode2400), r.TS1HeardCodec2Mode)
			require.NotNil(t, r.TS1HeardAt)
			assert.Empty(t, r.TS2HeardCodec, "the other slot heard nothing")
		})

		ambeCall := func(t *testing.T, from *testutils.MMDVMClient, src uint32, seed int) ([]models.Packet, tcGot, c2Call, uint) {
			t.Helper()
			c := call{group: true, dst: tcTG, src: src}
			stream := n.nextStream()
			cc := voiceCall(t, tcLC(t, c, enums.FIDStandardized), c, tcAMBESlots(seed))
			sent, g := n.send(t, from, cc, stream)
			return sent, g, cc, stream
		}

		step(t, "ambe-call-to-codec2-slots", func(t *testing.T) {
			before := n.ambe.Decodes()
			sent, g, cc, stream := ambeCall(t, n.ambeTalk, tcOther, 1)
			lc2 := cc.lc
			lc2.SetFID(0xC2)
			want := codec2CopySlots(t, codec2.Mode2400, cc.slots)
			requireCopy(t, sent, ofStream(g.c2Listen, stream), tcC2Listen, lc2, want)
			requireCopy(t, sent, ofStream(g.c2, stream), tcC2Rpt, lc2, want)
			assertSamePackets(t, sent, ofStream(g.ambeListen, stream), tcAMBERpt, false)
			assertSamePackets(t, sent, ofStream(g.ob, stream), tcOB, false)
			assertSamePackets(t, sent, ofStream(g.obCodec2, stream), tcOBCodec2, false)
			assert.Equal(t, tcFrames, n.ambe.Decodes()-before, "the call was not converted once")
			n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 2})
		})

		step(t, "an-ambe-call-that-lost-a-burst-keeps-its-timing", func(t *testing.T) {
			c := call{group: true, dst: tcTG, src: tcOther}
			stream := n.nextStream()
			cc := voiceCall(t, tcLC(t, c, enums.FIDStandardized), c, tcAMBESlots(5))
			all := cc.tx.packets(stream)
			const lost = 1 + 2
			sent := append(append([]models.Packet{}, all[:lost]...), all[lost+1:]...)
			good, repeat, silence := transcodeFrames(models.CodecAMBE, "good", "voice"), transcodeFrames(models.CodecAMBE, "lost", "repeat"), transcodeFrames(models.CodecAMBE, "lost", "silence")
			fills := n.ambe.LostFrames()
			sendC2(t, n.ambeTalk.SendDMRD, sent)
			g := n.drain(callWait(sent))
			got := ofStream(g.c2Listen, stream)
			require.Len(t, got, len(all), "the copy has a burst in place of the lost one")
			for i := range all {
				require.Equal(t, all[i].FrameType, got[i].FrameType, "packet %d frame type", i)
				require.Equal(t, all[i].DTypeOrVSeq, got[i].DTypeOrVSeq, "packet %d position", i)
				require.Equal(t, all[i].Seq, got[i].Seq, "packet %d sequence number", i)
			}
			assertSamePackets(t, sent, ofStream(g.ambeListen, stream), tcAMBERpt, false)
			assert.Equal(t, layer2.VoiceSlotsPerBurst, n.ambe.LostFrames()-fills)
			assert.GreaterOrEqual(t, transcodeFrames(models.CodecAMBE, "good", "voice")-good, float64(tcFrames-layer2.VoiceSlotsPerBurst))
			assert.GreaterOrEqual(t, transcodeFrames(models.CodecAMBE, "lost", "repeat")-repeat, 2.0)
			assert.GreaterOrEqual(t, transcodeFrames(models.CodecAMBE, "lost", "silence")-silence, 1.0)
			assert.Empty(t, n.ambe.Errors())
		})

		step(t, "ambe-call-clears-codec2", func(t *testing.T) {
			_, _, _, stream := ambeCall(t, n.c2, tcTalker, 2)
			n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
			r := n.heard(t, tcC2Rpt)
			assert.Equal(t, models.CodecAMBE, r.TS1HeardCodec)
			assert.Equal(t, int(codec2.Mode2400), r.TS1HeardCodec2Mode, "the mode stays for the next Codec2 call")

			require.NoError(t, stack.DB.Exec("UPDATE repeaters SET ts1_heard_codec2_mode = ? WHERE id = ?", int(codec2.Mode3200), tcC2Listen).Error)
			stack.Hub.CodecChanged(tcC2Listen)
			sent, g, cc, stream := ambeCall(t, n.ambeTalk, tcOther, 3)
			assertSamePackets(t, sent, ofStream(g.c2, stream), tcC2Rpt, false)
			lc2 := cc.lc
			lc2.SetFID(0xC2)
			requireCopy(t, sent, ofStream(g.c2Listen, stream), tcC2Listen, lc2, codec2CopySlots(t, codec2.Mode3200, cc.slots))
			n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
		})

		step(t, "voice-bits-never-make-codec2", func(t *testing.T) {
			before := n.heard(t, tcAMBETalkRpt)
			c := call{group: true, dst: tcTG, src: tcOther}
			stream := n.nextStream()
			cc := voiceCall(t, tcLC(t, c, enums.FIDStandardized), c, c2Slots(uint64(stream), tcFrames))
			sent := cc.tx.packets(stream)[1:]
			sendC2(t, n.ambeTalk.SendDMRD, sent)
			g := n.drain(callWait(sent))
			assertSamePackets(t, sent, ofStream(g.ambeListen, stream), tcAMBERpt, false)
			require.Len(t, ofStream(g.c2Listen, stream), len(sent), "the Codec2 slot gets the call as AMBE converted")
			n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
			after := n.heard(t, tcAMBETalkRpt)
			assert.Equal(t, models.CodecAMBE, before.TS1HeardCodec)
			assert.Equal(t, models.CodecAMBE, after.TS1HeardCodec, "Codec2 voice bits must not make the slot Codec2")
		})

		step(t, "a-codec2-call-whose-header-is-lost-teaches-the-slot", func(t *testing.T) {
			require.Equal(t, models.CodecAMBE, n.heard(t, tcC2Rpt).TS1HeardCodec)
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent := cc.tx.packets(stream)[1:]
			sendC2(t, n.c2.SendDMRD, sent)
			g := n.drain(callWait(sent))
			got := ofStream(g.ambeListen, stream)
			require.Len(t, got, len(sent))
			requireEmbeddedLC(t, got[12:], lc0)
			requireCallCodec(t, stack, stream, models.CodecCodec2, int(codec2.Mode2400))
			requireSlotHeard(t, stack, false, models.CodecCodec2, int(codec2.Mode2400))

			before := n.ambe.Decodes()
			sent, g, cc, stream = ambeCall(t, n.ambeTalk, tcOther, 4)
			lc2 := cc.lc
			lc2.SetFID(0xC2)
			requireCopy(t, sent, ofStream(g.c2, stream), tcC2Rpt, lc2, codec2CopySlots(t, codec2.Mode2400, cc.slots))
			assert.Equal(t, tcFrames, n.ambe.Decodes()-before, "the AMBE call was not converted once")
		})

		step(t, "a-terminator-that-fails-its-check-reaches-every-target", func(t *testing.T) {
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent := cc.tx.packets(stream)
			last := len(sent) - 1
			sent[last].DMRData = badHeader(layer2.RawBurst(sent[last].DMRData))
			sendC2(t, n.c2.SendDMRD, sent)
			g := n.drain(callWait(sent))
			got := ofStream(g.ambeListen, stream)
			require.Len(t, got, len(sent), "the AMBE target must get every burst")
			assert.Equal(t, sent[last].DMRData, got[last].DMRData, "a terminator without a copy goes out as it came")
		})

		step(t, "a-repeated-terminator-keeps-the-codec-of-the-call", func(t *testing.T) {
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent := cc.tx.packets(stream)
			sent = append(sent, sent[len(sent)-1])
			fromPeer := func(p models.Packet) error {
				p.Repeater = tcOB
				return n.ob.SendRawPacket(p)
			}
			sendC2(t, fromPeer, sent)
			g := n.drain(callWait(sent))
			got := ofStream(g.ambeListen, stream)
			require.Len(t, got, len(sent))
			for _, i := range []int{len(got) - 2, len(got) - 1} {
				requireC2LC(t, layer2.RawBurst(got[i].DMRData), enums.DataTypeTerminatorWithLC, lc0)
			}
		})

		step(t, "ambe-server-down", func(t *testing.T) {
			stack.Hub.Transcoder().SetTiming(transcode.Budget, transcode.AttemptTimeout)
			defer roomy(stack.Hub)
			missed := missedSeconds()
			n.ambe.SetAnswering(false)
			defer n.ambe.SetAnswering(true)
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent, g := n.send(t, n.c2, cc, stream)
			silence := make([][9]byte, tcFrames)
			for i := range silence {
				silence[i] = vocoder.AMBESilenceSlot()
			}
			requireCopy(t, sent, ofStream(g.ambeListen, stream), tcAMBERpt, lc0, silence)
			assertSamePackets(t, sent, ofStream(g.c2Listen, stream), tcC2Listen, false)
			n.requireRow(t, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400), sentAMBE: 4, missedMS: tcFrames * 20})
			assert.GreaterOrEqual(t, missedSeconds()-missed, float64(tcFrames)*0.02-1e-9)
		})

		step(t, "no-ambe-server", func(t *testing.T) {
			stack.Hub.SetAMBEServers(nil)
			defer stack.Hub.SetAMBEServers(ambeAt(n.ambe.Addr()))
			stream := n.nextStream()
			cc := newC2Call(t, group, false, tcBursts, uint64(stream))
			sent, g := n.send(t, n.c2, cc, stream)
			assertSamePackets(t, sent, ofStream(g.ambeListen, stream), tcAMBERpt, false)
			assertSamePackets(t, sent, ofStream(g.ob, stream), tcOB, false)
			n.requireRow(t, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400)})
		})
	})
}

// TestTranscodingPrivateCall sends a private Codec2 call to a radio on an
// AMBE hotspot, and its AMBE reply to the Codec2 hotspot. The private
// call also goes to the OpenBridge peers, the AMBE-only one converted.
func TestTranscodingPrivateCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		keyUp(t, n.ambeTalk, tcOther, tcTG, false, n.nextStream())
		n.drain(300 * time.Millisecond)

		c := call{dst: tcOther, src: tcTalker}
		stream := n.nextStream()
		cc := voiceCall(t, tcLC(t, c, 0xC2), c, c2Slots(uint64(stream), tcFrames))
		before := len(n.ambe.EncodedPCM())
		sent, g := n.send(t, n.c2, cc, stream)
		lc0 := cc.lc
		lc0.SetFID(enums.FIDStandardized)
		requireCopy(t, sent, ofStream(g.ambeTalk, stream), tcAMBETalkRpt, lc0, ambeCopySlots(t, n.ambe, before, cc.slots))
		assert.Equal(t, sent[0].DMRData, ofStream(g.obCodec2, stream)[0].DMRData, "the peer that carries Codec2 got a copy")
		assert.Equal(t, ofStream(g.ambeTalk, stream)[0].DMRData, ofStream(g.ob, stream)[0].DMRData, "the AMBE-only peer did not get the shared copy")
		n.requireRow(t, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400), sentAMBE: 2})

		r := call{dst: tcTalker, src: tcOther}
		stream = n.nextStream()
		rc := voiceCall(t, tcLC(t, r, enums.FIDStandardized), r, tcAMBESlots(9))
		sent, g = n.send(t, n.ambeTalk, rc, stream)
		lc2 := rc.lc
		lc2.SetFID(0xC2)
		requireCopy(t, sent, ofStream(g.c2, stream), tcC2Rpt, lc2, codec2CopySlots(t, codec2.Mode2400, rc.slots))
		n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
	})
}

// TestTranscodingPrivateCallWithoutHeader sends a private Codec2 call
// whose Voice LC Header is lost from the Codec2 hotspot, whose timeslot is
// AMBE. The embedded LC makes the call Codec2 and teaches the timeslot, so
// the AMBE reply reaches the caller converted to Codec2.
func TestTranscodingPrivateCallWithoutHeader(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		keyUp(t, n.ambeTalk, tcOther, tcTG, false, n.nextStream())
		n.drain(300 * time.Millisecond)

		c := call{dst: tcOther, src: tcTalker}
		stream := n.nextStream()
		cc := voiceCall(t, tcLC(t, c, 0xC2), c, c2Slots(uint64(stream), tcFrames))
		sent := cc.tx.packets(stream)[1:]
		sendC2(t, n.c2.SendDMRD, sent)
		g := n.drain(callWait(sent))
		lc0 := cc.lc
		lc0.SetFID(enums.FIDStandardized)
		got := ofStream(g.ambeTalk, stream)
		require.Len(t, got, len(sent))
		requireEmbeddedLC(t, got[12:], lc0)
		requireCallCodec(t, stack, stream, models.CodecCodec2, int(codec2.Mode2400))
		requireSlotHeard(t, stack, false, models.CodecCodec2, int(codec2.Mode2400))

		r := call{dst: tcTalker, src: tcOther}
		stream = n.nextStream()
		rc := voiceCall(t, tcLC(t, r, enums.FIDStandardized), r, tcAMBESlots(9))
		sent, g = n.send(t, n.ambeTalk, rc, stream)
		lc2 := rc.lc
		lc2.SetFID(0xC2)
		requireCopy(t, sent, ofStream(g.c2, stream), tcC2Rpt, lc2, codec2CopySlots(t, codec2.Mode2400, rc.slots))
		n.requireRow(t, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
	})
}

// TestTranscodingParrotWithoutHeader calls the parrot with a Codec2 call
// whose Voice LC Header is lost, from a timeslot that was AMBE. The reply
// must reach the caller in Codec2, as the parrot recorded it.
func TestTranscodingParrotWithoutHeader(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		runParrot(t, stack)
		c := call{dst: uint32(parrotID), src: tcTalker}
		stream := n.nextStream()
		cc := voiceCall(t, tcLC(t, c, 0xC2), c, c2Slots(uint64(stream), tcFrames))
		sent := cc.tx.packets(stream)[1:]
		sendC2(t, n.c2.SendDMRD, sent)

		var reply []models.Packet
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			got := ofCall(n.c2.Drain(time.Second), parrotID, tcTalker)
			reply = append(reply, got...)
			if len(reply) > 0 && len(got) == 0 {
				break
			}
		}
		require.Greater(t, len(reply), 2, "reply packets")

		want := cc.lc
		require.NoError(t, want.SetCall(false, tcTalker, uint32(parrotID)))
		requireC2LC(t, layer2.RawBurst(reply[0].DMRData), enums.DataTypeVoiceLCHeader, want)
		requireC2LC(t, layer2.RawBurst(reply[len(reply)-1].DMRData), enums.DataTypeTerminatorWithLC, want)
		recorded := map[[9]byte]bool{}
		for _, s := range cc.slots {
			recorded[s] = true
		}
		for i, p := range reply[1 : len(reply)-1] {
			require.True(t, isVoice(p), "packet %d is not voice", i+1)
			for k, s := range layer2.RawBurst(p.DMRData).VoiceSlots() {
				require.True(t, recorded[s], "packet %d slot %d %x is not a recorded Codec2 slot", i+1, k, s)
			}
		}
		requireCallCodec(t, stack, stream, models.CodecCodec2, int(codec2.Mode2400))
	})
}

// TestTranscodingAcrossReplicas puts the Codec2 hotspot on one replica and
// the AMBE hotspots on another. The replica of the caller converts the
// call once, the other delivers the copy, and the call row counts the
// target. What the first replica heard on the Codec2 hotspot reaches the
// second, which converts the AMBE call for it.
func TestTranscodingAcrossReplicas(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, tcTalker, "N0CALL")
		stack.SeedUser(t, tcOther, "N1CALL")
		stack.SeedTalkgroup(t, tcTG, "TG")
		stack.SeedMMDVMRepeater(t, tcC2Rpt, tcTalker, testPassword)
		stack.SeedMMDVMRepeater(t, tcAMBERpt, tcTalker, testPassword)
		stack.SeedMMDVMRepeater(t, tcAMBETalkRpt, tcOther, testPassword)
		stack.AssignTS1StaticTG(t, tcAMBERpt, tcTG)
		second := stack.SpawnSecondReplica(t)
		stack.StartServers(t)
		srv := testutils.StartFakeAMBEServer(t)
		roomy(stack.Hub)
		roomy(second.Hub)
		stack.Hub.SetAMBEServers(ambeAt(srv.Addr()))
		second.Hub.SetAMBEServers(ambeAt(srv.Addr()))

		connect := func(id uint, at string) *testutils.MMDVMClient {
			c := testutils.NewMMDVMClient(uint32(id), "", testPassword) //nolint:gosec
			t.Cleanup(c.Close)
			require.NoError(t, c.Connect(at))
			require.NoError(t, c.WaitReady(handshakeWait))
			return c
		}
		c2 := connect(tcC2Rpt, stack.MMDVMAddr)
		listen := connect(tcAMBERpt, second.MMDVMAddr)
		talk := connect(tcAMBETalkRpt, second.MMDVMAddr)
		time.Sleep(settleDuration)
		drainQuiet(500*time.Millisecond, c2, listen, talk)

		group := call{group: true, dst: tcTG, src: tcTalker}
		stream := uint(0xB0010)
		cc := newC2Call(t, group, false, tcBursts, uint64(stream))
		sent := cc.tx.packets(stream)
		sendC2(t, c2.SendDMRD, sent)
		got := drainQuiet(callWait(sent), c2, listen, talk)
		requireCopy(t, sent, ofStream(got[1], stream), tcAMBERpt, tcLC(t, group, enums.FIDStandardized), ambeCopySlots(t, srv, 0, cc.slots))
		requireCodecRow(t, stack, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400), sentAMBE: 1})

		a := call{group: true, dst: tcTG, src: tcOther}
		stream = 0xB0020
		ac := voiceCall(t, tcLC(t, a, enums.FIDStandardized), a, tcAMBESlots(5))
		sent = ac.tx.packets(stream)
		sendC2(t, talk.SendDMRD, sent)
		got = drainQuiet(callWait(sent), c2, listen, talk)
		lc2 := ac.lc
		lc2.SetFID(0xC2)
		requireCopy(t, sent, ofStream(got[0], stream), tcC2Rpt, lc2, codec2CopySlots(t, codec2.Mode2400, ac.slots))
		assertSamePackets(t, sent, ofStream(got[1], stream), tcAMBERpt, false)
		requireCodecRow(t, stack, stream, tcRow{codec: models.CodecAMBE, sentCodec2: 1})
	})
}

// TestDrainingWaitsForTheCopies makes the AMBE server slow and sends the
// last two voice bursts and the terminator of a Codec2 call at once, so
// that their copies are still being made when the call ends. Waiting for
// the calls in flight, as a replica does before it stops, must wait for the
// copies too.
func TestDrainingWaitsForTheCopies(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		n.ambe.SetDelay(8 * time.Millisecond)
		before := n.ambe.Encodes()
		stream := n.nextStream()
		cc := newC2Call(t, call{group: true, dst: tcTG, src: tcTalker}, false, tcBursts, uint64(stream))
		sent := cc.tx.packets(stream)
		for i, p := range sent {
			require.NoError(t, n.c2.SendDMRD(p))
			if i < len(sent)-3 {
				time.Sleep(60 * time.Millisecond)
			}
		}
		require.Eventually(t, func() bool {
			var calls []models.Call
			return stack.DB.Where("stream_id = ?", stream).Find(&calls).Error == nil && len(calls) == 1 && !calls[0].Active
		}, tcRowWait, 5*time.Millisecond, "the call ended")
		stack.Hub.WaitForCalls()
		drained := n.ambe.Encodes() - before
		time.Sleep(500 * time.Millisecond)
		assert.Equal(t, drained, n.ambe.Encodes()-before, "copies were still being made after the calls in flight ended")
		assert.Positive(t, drained)
	})
}

// TestABusyAMBEServerLeavesACallUnconverted sends two Codec2 calls at once
// from both slots of the Codec2 hotspot, with one AMBEserver that takes one
// call. One call reaches the AMBE hotspot converted; the other is not
// converted: the AMBE hotspot gets none of it, the Codec2 slot gets it as
// it came, and its row says every AMBE server was busy.
func TestABusyAMBEServerLeavesACallUnconverted(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		n.ambe.SetLastClientOnly(true)
		stack.Hub.SetAMBEServers([]transcode.ServerConfig{{Addr: n.ambe.Addr(), Calls: 1}})
		require.Eventually(t, func() bool {
			st := stack.Hub.Transcoder().Status(t.Context())
			return len(st) == 1 && st[0].Answered
		}, 5*time.Second, 10*time.Millisecond)
		busy := missedBusy()

		group := call{group: true, dst: tcTG, src: tcTalker}
		s1, s2 := n.nextStream(), n.nextStream()
		c1 := newC2Call(t, group, false, tcBursts, uint64(s1))
		c2 := newC2Call(t, group, true, tcBursts, uint64(s2))
		p1, p2 := c1.tx.packets(s1), c2.tx.packets(s2)
		for i := range p1 {
			require.NoError(t, n.c2.SendDMRD(p1[i]))
			require.NoError(t, n.c2.SendDMRD(p2[i]))
			time.Sleep(30 * time.Millisecond)
		}
		g := n.drain(callWait(p1))

		rows := map[uint]models.Call{}
		require.Eventually(t, func() bool {
			var calls []models.Call
			if stack.DB.Where("stream_id IN ? AND kind = ?", []uint{s1, s2}, models.CallKindVoice).Find(&calls).Error != nil || len(calls) != 2 {
				return false
			}
			for _, c := range calls {
				if c.Active {
					return false
				}
				rows[c.StreamID] = c
			}
			return true
		}, tcRowWait, 50*time.Millisecond)
		conv, rest, restSent := s1, s2, p2
		if rows[s1].TranscodeBusy {
			conv, rest, restSent = s2, s1, p1
		}
		require.True(t, rows[rest].TranscodeBusy, "neither call found the server busy")
		require.False(t, rows[conv].TranscodeBusy, "both calls found the server busy")
		assert.Zero(t, rows[conv].MissedMS, "the converted call lost audio")
		assert.Equal(t, 3, rows[conv].SentAMBE, "the AMBE hotspot, IPSC and the AMBE-only peer")
		assert.Zero(t, rows[rest].SentAMBE)

		assert.Len(t, ofStream(g.ambeListen, conv), len(p1), "the converted call reaches the AMBE hotspot")
		assert.Empty(t, ofStream(g.ambeListen, rest), "an AMBE hotspot got a call it cannot play")
		assert.Empty(t, ofStream(g.ob, rest), "the AMBE-only peer got a call it cannot carry")
		assertSamePackets(t, restSent, ofStream(g.obCodec2, rest), tcOBCodec2, false)
		got := ofStream(g.c2Listen, rest)
		require.Len(t, got, len(restSent))
		for i := range got {
			assert.Equal(t, restSent[i].DMRData, got[i].DMRData, "packet %d of the call that was not converted", i)
		}
		assert.GreaterOrEqual(t, missedBusy()-busy, float64(tcFrames)*0.02-1e-9, "the audio of the call that was not converted is counted")
		assert.Empty(t, n.ambe.Errors())
	})
}

// missedBusy returns the audio counted as busy, and as late: a burst
// that waits behind a full queue is counted late before the transcoder
// sees that the call has no server.
func missedBusy() float64 {
	m := metrics.NewMetrics().TranscodeMissedSecondsTotal
	return testutil.ToFloat64(m.WithLabelValues(models.CodecAMBE, transcode.ReasonBusy)) + testutil.ToFloat64(m.WithLabelValues(models.CodecAMBE, transcode.ReasonLate))
}

// roomy gives the calls that the hub converts 2 s for each burst and
// 500 ms for each answer of the AMBE server. The tests that check what a
// conversion makes, rather than when, run next to many other tests on CI
// machines with 2 to 4 cores, where the race detector and the coverage
// counters slow every goroutine; there the fake AMBE server and the
// transcoder can wait longer than the 18 ms of an attempt, and frames went
// out as silence.
func roomy(h *hub.Hub) {
	h.Transcoder().SetTiming(2*time.Second, 500*time.Millisecond)
}

// TestABacklogWithinTheBudgetLosesNoBurst makes the AMBE server answer
// after 30 ms, so each burst takes about 90 ms to convert while the call
// sends one every 30 ms. With a 10 s budget every burst still fits it, so
// the queue of the transcoder must hold the backlog: the AMBE hotspot gets
// every burst, and no audio is lost.
func TestABacklogWithinTheBudgetLosesNoBurst(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		stack.Hub.Transcoder().SetTiming(10*time.Second, 500*time.Millisecond)
		n.ambe.SetDelay(30 * time.Millisecond)
		stream := n.nextStream()
		cc := newC2Call(t, call{group: true, dst: tcTG, src: tcTalker}, false, tcBursts, uint64(stream))
		sent, g := n.send(t, n.c2, cc, stream)
		assert.Len(t, ofStream(g.ambeListen, stream), len(sent), "bursts were dropped behind a full queue")
		n.requireRow(t, stream, tcRow{codec: models.CodecCodec2, mode: int(codec2.Mode2400), sentAMBE: 3})
	})
}

// TestTheCopiesOfBackToBackCallsKeepTheirOrder sends two Codec2 calls from
// one hotspot slot back to back while the AMBE server answers after 30 ms,
// so the copies of the first call are still made when the second starts.
// The AMBE hotspot must get every copy of the first call before any copy
// of the second.
func TestTheCopiesOfBackToBackCallsKeepTheirOrder(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		n := newTCNet(t, stack)
		stack.Hub.Transcoder().SetTiming(10*time.Second, 500*time.Millisecond)
		n.ambe.SetDelay(30 * time.Millisecond)
		first, second := n.nextStream(), n.nextStream()
		sent := make([]models.Packet, 0, 2*(tcBursts+2))
		for _, stream := range []uint{first, second} {
			cc := newC2Call(t, call{group: true, dst: tcTG, src: tcTalker}, false, tcBursts, uint64(stream))
			sent = append(sent, cc.tx.packets(stream)...)
		}
		sendC2(t, n.c2.SendDMRD, sent)
		g := n.drain(callWait(sent))

		var order []uint
		for _, p := range g.ambeListen {
			if p.StreamID == first || p.StreamID == second {
				order = append(order, p.StreamID)
			}
		}
		require.Len(t, order, len(sent), "the AMBE hotspot missed copies")
		for i := 1; i < len(order); i++ {
			if order[i] == first && order[i-1] == second {
				t.Fatalf("a copy of the first call came after a copy of the second: %v", order)
			}
		}
	})
}
