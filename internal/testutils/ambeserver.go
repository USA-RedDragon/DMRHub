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
const FakeAMBEProduct = "AMBE3000R"

// FakeAMBEServer is an AMBEserver on loopback for tests: it answers the
// packets of the DVSI chip protocol, one per UDP datagram, to the address
// that sent each one. It decodes a frame to FakeAMBEDecode of it and
// encodes audio to FakeAMBEEncode of it, so a test can tell what a
// transcoder made of each frame.
type FakeAMBEServer struct {
	conn    *net.UDPConn
	answer  atomic.Bool
	lastOne atomic.Bool
	last    atomic.Pointer[net.UDPAddr]
	drop    atomic.Int64
	delay   atomic.Int64
	decodes atomic.Int64
	encodes atomic.Int64
	queries atomic.Int64
	bare    atomic.Bool
	mu      sync.Mutex
	errs    []string
	askers  map[string]bool
	pcm     [][dvsi.FrameSamples]int16
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

// SetLastClientOnly makes the server answer as an AMBEserver with one DVSI
// chip does: each answer goes to the client whose request came last, and
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

// SetDelay makes the server wait d before each reply.
func (f *FakeAMBEServer) SetDelay(d time.Duration) {
	f.delay.Store(int64(d))
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
		if reply == nil || !f.answer.Load() {
			continue
		}
		if reply[3] != byte(dvsi.TypeControl) && f.drop.Load() > 0 && f.drop.Add(-1) >= 0 {
			continue
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
		if len(p.Fields) != 11 || p.Fields[0] != 0x01 || p.Fields[1] != dvsi.FrameBits {
			f.fail("channel packet % x is not one 72-bit frame", p.Fields)
			return nil
		}
		if answering {
			f.decodes.Add(1)
		}
		pcm := FakeAMBEDecode([9]byte(p.Fields[2:11]))
		out, err := dvsi.AppendSpeech(nil, 0, pcm[:], true)
		if err != nil {
			f.fail("speech reply: %v", err)
			return nil
		}
		return out
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
		case dvsi.FieldReset:
			f.fail("the hub reset the chip, which other calls may be using")
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
	n := len(fields) + 2
	b := append([]byte{dvsi.StartByte, byte(n >> 8), byte(n), byte(dvsi.TypeControl)}, fields...)
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
