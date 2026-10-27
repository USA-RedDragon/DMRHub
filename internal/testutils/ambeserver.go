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

package testutils

import (
	"bytes"
	"errors"
	"fmt"
	"hash/fnv"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder/dvsi"
)

// FakeAMBEProduct is the product name that a FakeAMBEServer reports.
const FakeAMBEProduct = "FAKEAMBE"

// The DCMODE_IN and DCMODE_OUT flags of the CMODE field that a
// FakeAMBEServer takes in channel packets and puts in speech packets
// (the CMODE field of the AMBEserver protocol).
const (
	AMBELostFrame   = 0x0004
	AMBEVoiceActive = 0x0002
	AMBEDataInvalid = 0x0020
	AMBEToneFrame   = 0x8000
)

const (
	fieldCMode        = 0x02
	fieldDCMode       = 0x06
	fieldSpeechFormat = 0x16
	maxRepeats        = 2
)

// FakeAMBEServer is an AMBEserver on loopback for tests: it answers the
// packets of the AMBEserver protocol, one per UDP datagram, to the address
// that sent each one. It decodes a frame to FakeAMBEDecode of it and
// encodes audio to FakeAMBEEncode of it, so a test can tell what a
// transcoder made of each frame. After a PKT_SPCHFMT field of 0x0001 it
// puts the decoder status in a CMODE field of each speech packet. A channel
// packet with a CMODE field that sets LOST_FRAME, with or without a CHAND
// field, makes it repeat the audio of the last frame, up to 2 times, and
// then send silence as comfort noise. Like the chip, it keeps LOST_FRAME
// for all clients until a CMODE field or a PKT_DCMODE field of 0x0000
// clears it. Like the deployed AMBEserver, it drops a channel packet whose
// CMODE sets any other flag, and records the error; it does the same for a
// PKT_DCMODE field.
type FakeAMBEServer struct {
	conn    *net.UDPConn
	answer  atomic.Bool
	lastOne atomic.Bool
	last    atomic.Pointer[net.UDPAddr]
	drop    atomic.Int64
	onDrop  atomic.Pointer[func()]
	onReply atomic.Pointer[func()]
	delay   atomic.Int64
	decodes atomic.Int64
	encodes atomic.Int64
	queries atomic.Int64
	bare    atomic.Bool
	format  atomic.Bool
	refuse  atomic.Bool
	losts   atomic.Int64
	cmodes  atomic.Int64
	dcmodes atomic.Int64
	lost    bool
	repeats int
	lastPCM [dvsi.FrameSamples]int16
	mu      sync.Mutex
	errs    []string
	askers  map[string]bool
	callers map[string]bool
	pcm     [][dvsi.FrameSamples]int16
	status  map[[9]byte]uint16
	done    chan struct{}
}

// StartFakeAMBEServer starts a FakeAMBEServer on 127.0.0.1 that answers,
// and stops it when the test ends.
func StartFakeAMBEServer(t testing.TB) *FakeAMBEServer {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatalf("fake AMBE server: %v", err)
	}
	f := &FakeAMBEServer{conn: conn, done: make(chan struct{})}
	f.answer.Store(true)
	go f.serve()
	t.Cleanup(f.Close)
	return f
}

// Addr returns the host and port of the server.
func (f *FakeAMBEServer) Addr() string {
	return f.conn.LocalAddr().String()
}

// SetAnswering makes the server answer requests or drop them all.
func (f *FakeAMBEServer) SetAnswering(on bool) {
	f.answer.Store(on)
}

// SetLastClientOnly makes the server answer as an AMBE server with one chip
// does: each answer goes to the client whose request came last, and
// it takes 3 ms. Two clients at once then get each other's answers.
func (f *FakeAMBEServer) SetLastClientOnly(on bool) {
	f.lastOne.Store(on)
}

// SetControlParity makes the server end its control replies with the
// parity field or leave it out, as a chip with parity mode off does.
func (f *FakeAMBEServer) SetControlParity(on bool) {
	f.bare.Store(!on)
}

// DropNext makes the server drop the replies to the next n frame
// requests, as a lost datagram.
func (f *FakeAMBEServer) DropNext(n int) {
	f.drop.Store(int64(n))
}

// SetDropHook makes the server call hook each time it drops a reply for
// DropNext, and each time it does not answer a client that has sent it
// frames, before the client can time out, so that a test that models time
// can count the wait of the client.
func (f *FakeAMBEServer) SetDropHook(hook func()) {
	f.onDrop.Store(&hook)
}

// SetReplyHook makes the server call hook before each reply to a client
// that has sent it frames, so that a test that models time can count the
// time that the chip takes to answer.
func (f *FakeAMBEServer) SetReplyHook(hook func()) {
	f.onReply.Store(&hook)
}

func runHook(hook *func()) {
	if hook != nil && *hook != nil {
		(*hook)()
	}
}

// caller reports whether from has sent the server frames, and records it
// when b is a frame.
func (f *FakeAMBEServer) caller(from string, b []byte) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b[3] == byte(dvsi.TypeSpeech) || b[3] == byte(dvsi.TypeChannel) {
		if f.callers == nil {
			f.callers = map[string]bool{}
		}
		f.callers[from] = true
	}
	return f.callers[from]
}

// SetDelay makes the server wait d before each reply.
func (f *FakeAMBEServer) SetDelay(d time.Duration) {
	f.delay.Store(int64(d))
}

// SetRefuseSpeechFormat makes the server answer a PKT_SPCHFMT field with
// an error status, as an AMBEserver that cannot report the decoder status.
func (f *FakeAMBEServer) SetRefuseSpeechFormat(on bool) {
	f.refuse.Store(on)
}

// SetDecodeStatus makes the server report the DCMODE_OUT flags for each
// later decode of slot, in place of AMBEVoiceActive.
func (f *FakeAMBEServer) SetDecodeStatus(slot [9]byte, flags uint16) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.status == nil {
		f.status = map[[9]byte]uint16{}
	}
	f.status[slot] = flags
}

// SpeechFormat reports whether a client set the speech format that puts
// the decoder status in each speech packet.
func (f *FakeAMBEServer) SpeechFormat() bool {
	return f.format.Load()
}

// LostFrames returns the number of frames that the server filled while
// LOST_FRAME was set.
func (f *FakeAMBEServer) LostFrames() int {
	return int(f.losts.Load())
}

// CModes returns the number of channel packets with a CMODE field that
// the server took.
func (f *FakeAMBEServer) CModes() int {
	return int(f.cmodes.Load())
}

// DCModes returns the number of PKT_DCMODE fields that the server took.
func (f *FakeAMBEServer) DCModes() int {
	return int(f.dcmodes.Load())
}

// Decodes returns the number of frames that the server decoded.
func (f *FakeAMBEServer) Decodes() int {
	return int(f.decodes.Load())
}

// Encodes returns the number of frames that the server encoded.
func (f *FakeAMBEServer) Encodes() int {
	return int(f.encodes.Load())
}

// Queries returns the number of product queries that the server answered.
func (f *FakeAMBEServer) Queries() int {
	return int(f.queries.Load())
}

// QuerySources returns the number of client addresses that sent product
// queries.
func (f *FakeAMBEServer) QuerySources() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.askers)
}

// EncodedPCM returns the audio of each frame that the server encoded, in
// the order it came.
func (f *FakeAMBEServer) EncodedPCM() [][dvsi.FrameSamples]int16 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][dvsi.FrameSamples]int16(nil), f.pcm...)
}

// Errors returns the protocol errors found in the packets that came in.
func (f *FakeAMBEServer) Errors() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.errs...)
}

// Close stops the server.
func (f *FakeAMBEServer) Close() {
	select {
	case <-f.done:
		return
	default:
	}
	close(f.done)
	_ = f.conn.Close()
}

func (f *FakeAMBEServer) fail(format string, args ...any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.errs = append(f.errs, fmt.Sprintf(format, args...))
}

func (f *FakeAMBEServer) serve() {
	buf := make([]byte, dvsi.MaxPacketLen+16)
	for {
		n, from, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		f.last.Store(from)
		reply := f.handle(buf[:n])
		if reply != nil && reply[3] == byte(dvsi.TypeControl) && bytes.IndexByte(buf[4:n], byte(dvsi.FieldProdID)) >= 0 {
			f.mu.Lock()
			if f.askers == nil {
				f.askers = map[string]bool{}
			}
			f.askers[from.String()] = true
			f.mu.Unlock()
		}
		if reply == nil {
			continue
		}
		caller := f.caller(from.String(), buf[:n])
		if !f.answer.Load() {
			if caller {
				runHook(f.onDrop.Load())
			}
			continue
		}
		if reply[3] != byte(dvsi.TypeControl) && f.drop.Load() > 0 && f.drop.Add(-1) >= 0 {
			runHook(f.onDrop.Load())
			continue
		}
		if caller {
			runHook(f.onReply.Load())
		}
		if f.lastOne.Load() {
			go func(b []byte) {
				time.Sleep(3 * time.Millisecond)
				_, _ = f.conn.WriteToUDP(b, f.last.Load())
			}(reply)
			continue
		}
		if d := time.Duration(f.delay.Load()); d > 0 {
			go func(b []byte, to *net.UDPAddr) {
				time.Sleep(d)
				_, _ = f.conn.WriteToUDP(b, to)
			}(reply, from)
			continue
		}
		_, _ = f.conn.WriteToUDP(reply, from)
	}
}

func (f *FakeAMBEServer) handle(b []byte) []byte {
	p, n, err := dvsi.Parse(b, true)
	if err != nil || n != len(b) {
		f.fail("datagram of %d bytes is not one packet with parity: %v", len(b), err)
		return nil
	}
	answering := f.answer.Load()
	switch p.Type {
	case dvsi.TypeControl:
		return f.control(p.Fields, answering)
	case dvsi.TypeChannel:
		return f.channel(p.Fields, answering)
	case dvsi.TypeSpeech:
		if len(p.Fields) != 2+2*dvsi.FrameSamples || p.Fields[0] != 0x00 || p.Fields[1] != dvsi.FrameSamples {
			f.fail("speech packet of %d bytes is not one frame of 160 samples", len(p.Fields))
			return nil
		}
		var pcm [dvsi.FrameSamples]int16
		for i := range pcm {
			pcm[i] = int16(uint16(p.Fields[2+2*i])<<8 | uint16(p.Fields[3+2*i])) //nolint:gosec
		}
		if answering {
			f.encodes.Add(1)
			f.mu.Lock()
			f.pcm = append(f.pcm, pcm)
			f.mu.Unlock()
		}
		slot := FakeAMBEEncode(pcm[:])
		out, err := dvsi.AppendChannel(nil, 0, slot[:], dvsi.FrameBits, true)
		if err != nil {
			f.fail("channel reply: %v", err)
			return nil
		}
		return out
	default:
		f.fail("packet type %d", p.Type)
		return nil
	}
}

func (f *FakeAMBEServer) channel(fields []byte, answering bool) []byte {
	var chand []byte
	var mode uint16
	hasMode := false
	for b := fields; len(b) > 0; {
		switch {
		case chand == nil && len(b) >= 11 && b[0] == 0x01 && b[1] == dvsi.FrameBits:
			chand, b = b[2:11], b[11:]
		case !hasMode && len(b) >= 3 && b[0] == fieldCMode:
			mode, hasMode, b = uint16(b[1])<<8|uint16(b[2]), true, b[3:]
		default:
			f.fail("channel packet % x is not one 72-bit frame and a CMODE field", fields)
			return nil
		}
	}
	if hasMode {
		if mode&^AMBELostFrame != 0 {
			f.fail("CMODE 0x%04x sets flags other than LOST_FRAME", mode)
			return nil
		}
		f.cmodes.Add(1)
		f.lost = mode&AMBELostFrame != 0
	}
	var pcm [dvsi.FrameSamples]int16
	var flags uint16
	switch {
	case f.lost:
		if answering {
			f.losts.Add(1)
		}
		f.repeats++
		flags = AMBEDataInvalid
		if f.repeats <= maxRepeats {
			pcm, flags = f.lastPCM, flags|AMBEVoiceActive
		}
	case chand == nil:
		f.fail("channel packet % x has no CHAND field and LOST_FRAME is not set", fields)
		return nil
	default:
		if answering {
			f.decodes.Add(1)
		}
		slot := [9]byte(chand)
		pcm, flags = FakeAMBEDecode(slot), AMBEVoiceActive
		f.mu.Lock()
		if st, ok := f.status[slot]; ok {
			flags = st
		}
		f.mu.Unlock()
		f.repeats, f.lastPCM = 0, pcm
	}
	r := make([]byte, 0, 2+2*dvsi.FrameSamples+3)
	r = append(r, 0x00, dvsi.FrameSamples)
	for _, v := range pcm {
		r = append(r, byte(uint16(v)>>8), byte(v)) //nolint:gosec
	}
	if f.format.Load() {
		r = append(r, fieldCMode, byte(flags>>8), byte(flags))
	}
	return packet(dvsi.TypeSpeech, r)
}

func (f *FakeAMBEServer) control(fields []byte, answering bool) []byte {
	var r []byte
	for len(fields) > 0 {
		id := fields[0]
		fields = fields[1:]
		switch dvsi.Field(id) { //nolint:exhaustive // the hub sends only these fields on channel 0
		case dvsi.FieldChannel0:
			r = append(r, id, 0)
		case dvsi.FieldParityMode:
			if len(fields) < 1 || fields[0] != 1 {
				f.fail("parity mode % x: the hub must keep parity on", fields)
				return nil
			}
			r = append(r, id, 0)
			fields = fields[1:]
		case dvsi.FieldProdID:
			if answering {
				f.queries.Add(1)
			}
			r = append(append(r, id), FakeAMBEProduct...)
			r = append(r, 0)
		case dvsi.FieldVerString:
			r = append(append(r, id), "V120.E100.XXXX.C106.G514.R009.B0010411.C0020208"...)
			r = append(r, 0)
		case dvsi.FieldRateP:
			if len(fields) < 12 {
				f.fail("short RATEP")
				return nil
			}
			if [12]byte(fields[:12]) != [12]byte{0x04, 0x31, 0x07, 0x54, 0x24, 0x00, 0x00, 0x00, 0x00, 0x00, 0x6F, 0x48} {
				f.fail("RATEP % x is not the DMR rate", fields[:12])
			}
			r = append(r, id, 0)
			fields = fields[12:]
		case dvsi.FieldInit:
			if len(fields) < 1 {
				f.fail("short INIT")
				return nil
			}
			r = append(r, id, 0)
			fields = fields[1:]
		case fieldSpeechFormat:
			if len(fields) < 2 || fields[0] != 0x00 || fields[1] != 0x01 {
				f.fail("PKT_SPCHFMT % x is not 0x0001", fields)
				return nil
			}
			st := byte(0)
			if f.refuse.Load() {
				st = 1
			} else {
				f.format.Store(true)
			}
			r = append(r, id, st)
			fields = fields[2:]
		case fieldDCMode:
			if len(fields) < 2 {
				f.fail("short PKT_DCMODE")
				return nil
			}
			mode := uint16(fields[0])<<8 | uint16(fields[1])
			if mode&^AMBELostFrame != 0 {
				f.fail("PKT_DCMODE 0x%04x sets flags other than LOST_FRAME", mode)
				return nil
			}
			f.dcmodes.Add(1)
			f.lost = mode&AMBELostFrame != 0
			r = append(r, id, 0)
			fields = fields[2:]
		case dvsi.FieldReset:
			f.fail("the hub reset the chip, which other calls may be using")
			f.format.Store(false)
			f.lost, f.repeats = false, 0
			return controlPacket([]byte{byte(dvsi.FieldReady)})
		default:
			f.fail("control field 0x%02x", id)
			return nil
		}
	}
	if f.bare.Load() {
		return append([]byte{dvsi.StartByte, byte(len(r) >> 8), byte(len(r)), byte(dvsi.TypeControl)}, r...)
	}
	return controlPacket(r)
}

func controlPacket(fields []byte) []byte {
	return packet(dvsi.TypeControl, fields)
}

func packet(t dvsi.PacketType, fields []byte) []byte {
	n := len(fields) + 2
	b := append([]byte{dvsi.StartByte, byte(n >> 8), byte(n), byte(t)}, fields...)
	b = append(b, 0x2F)
	var x byte
	for _, c := range b[1:] {
		x ^= c
	}
	return append(b, x)
}

// FakeAMBEDecode returns the audio that a FakeAMBEServer decodes slot to.
func FakeAMBEDecode(slot [9]byte) [dvsi.FrameSamples]int16 {
	var pcm [dvsi.FrameSamples]int16
	for j := range pcm {
		pcm[j] = int16(int(slot[j%9])*48 - 6000 + (j*37)%400) //nolint:gosec
	}
	return pcm
}

// FakeAMBEEncode returns the AMBE+2 slot, with its FEC, that a
// FakeAMBEServer encodes pcm to.
func FakeAMBEEncode(pcm []int16) [9]byte {
	h := fnv.New64a()
	for _, s := range pcm {
		_, _ = h.Write([]byte{byte(s >> 8), byte(s)})
	}
	sum := h.Sum64()
	var frame vocoder.AMBEFrame
	for i := range 6 {
		frame[i] = byte(sum >> (8 * i))
	}
	frame[6] = byte(sum>>48) & 0x80
	slot, _ := vocoder.EncodeAMBE72(frame)
	return slot
}
