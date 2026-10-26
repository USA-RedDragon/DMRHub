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

// Package modemtest is a fake MMDVM modem: the firmware side of the MMDVM
// serial protocol, as MMDVM and MMDVM_HS answer the host, for tests.
package modemtest

import (
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

// FIFOBursts is the room of each TX FIFO of the fake in bursts.
const FIFOBursts = 10

// The NAK reasons of the firmware.
const (
	nakUnknown = 1
	nakState   = 2
	nakInvalid = 4
	nakFull    = 5
)

const (
	configV2Len = 37
	configV1Len = 23
	maxTXDelay  = 50
	maxColor    = 15
)

var errClosed = errors.New("fake modem closed")

// Fake is a modem on one end of a connection. Make it with New.
type Fake struct {
	conn     io.ReadWriteCloser
	protocol uint8
	desc     string

	silent    atomic.Bool
	nakConfig atomic.Bool

	mu       sync.Mutex
	changed  chan struct{}
	got      []mmdvm.Frame
	configs  int
	duplex   bool
	mode     mmdvm.Mode
	tx       bool
	debug    bool
	cc       enums.ColorCode
	fifo     [2][]layer2.RawBurst
	sent     [2][]layer2.RawBurst
	shortLCs []mmdvm.DMRShortLC

	wmu      sync.Mutex
	done     chan struct{}
	closeOne sync.Once
	wg       sync.WaitGroup
}

// New starts a fake modem of protocol 1 (MMDVM_HS) or 2 (MMDVM) on conn.
// Close stops it.
func New(conn io.ReadWriteCloser, protocol uint8) *Fake {
	f := &Fake{conn: conn, protocol: protocol, desc: "MMDVM fake 20261026", changed: make(chan struct{}),
		done: make(chan struct{})}
	f.wg.Add(2)
	go f.read()
	go f.transmit()
	return f
}

// Close stops the fake and closes its connection.
func (f *Fake) Close() {
	f.closeOne.Do(func() {
		close(f.done)
		_ = f.conn.Close()
	})
	f.wg.Wait()
}

// SetSilent makes the fake stop answering status requests, as a modem
// that hangs.
func (f *Fake) SetSilent(on bool) { f.silent.Store(on) }

// SetNAKConfig makes the fake refuse SET_CONFIG.
func (f *Fake) SetNAKConfig(on bool) { f.nakConfig.Store(on) }

func (f *Fake) notify() {
	close(f.changed)
	f.changed = make(chan struct{})
}

// WaitFor waits until cond, which runs with the state of the fake locked,
// is true, for at most timeout, and reports whether it was.
func (f *Fake) WaitFor(timeout time.Duration, cond func(f *Fake) bool) bool {
	t := time.NewTimer(timeout)
	defer t.Stop()
	for {
		f.mu.Lock()
		ok := cond(f)
		ch := f.changed
		f.mu.Unlock()
		if ok {
			return true
		}
		select {
		case <-ch:
		case <-t.C:
			return false
		case <-f.done:
			return false
		}
	}
}

// Frames returns the frames that the host sent, oldest first. Call this
// and the other getters in a WaitFor condition, which holds the lock of
// the fake.
func (f *Fake) Frames() []mmdvm.Frame { return f.got }

// Count returns how many frames of command c the host sent.
func (f *Fake) Count(c mmdvm.Command) int {
	n := 0
	for _, g := range f.got {
		if g.Command == c {
			n++
		}
	}
	return n
}

// Transmitted returns the bursts that the fake sent on the air on slot s,
// or on the DMO channel for slot 2 in simplex.
func (f *Fake) Transmitted(s enums.Slot) []layer2.RawBurst {
	if s != enums.Slot1 && s != enums.Slot2 {
		return nil
	}
	return f.sent[s-1]
}

// ShortLCs returns the Short LCs that the host sent.
func (f *Fake) ShortLCs() []mmdvm.DMRShortLC { return f.shortLCs }

// TX reports whether the duplex transmitter is on.
func (f *Fake) TX() bool { return f.tx }

// Mode returns the mode of the fake.
func (f *Fake) Mode() mmdvm.Mode { return f.mode }

// Configured returns how many SET_CONFIG frames the fake accepted.
func (f *Fake) Configured() int { return f.configs }

// Duplex reports whether the last SET_CONFIG was duplex.
func (f *Fake) Duplex() bool { return f.duplex }

// Write sends raw octets to the host.
func (f *Fake) Write(p []byte) error {
	f.wmu.Lock()
	defer f.wmu.Unlock()
	select {
	case <-f.done:
		return errClosed
	default:
	}
	_, err := f.conn.Write(p)
	return err //nolint:wrapcheck // a test helper
}

func (f *Fake) reply(c mmdvm.Command, payload []byte) {
	b, err := mmdvm.AppendFrame(nil, c, payload)
	if err == nil {
		_ = f.Write(b)
	}
}

func (f *Fake) ack(c mmdvm.Command) { f.reply(mmdvm.CommandAck, []byte{byte(c)}) }

func (f *Fake) nak(c mmdvm.Command, reason uint8) {
	f.reply(mmdvm.CommandNak, []byte{byte(c), reason})
}

// SendDMR sends a burst of slot s to the host, as the firmware sends what
// it received.
func (f *Fake) SendDMR(s enums.Slot, control mmdvm.Control, burst layer2.RawBurst) error {
	d := mmdvm.DMRData{Slot: s, Control: control, Burst: burst}
	return f.sendData(&d)
}

// SendLost tells the host that the signal of slot s is lost.
func (f *Fake) SendLost(s enums.Slot) error {
	d := mmdvm.DMRData{Slot: s, Lost: true}
	return f.sendData(&d)
}

func (f *Fake) sendData(d *mmdvm.DMRData) error {
	p, err := d.AppendBinary(nil)
	if err != nil {
		return err //nolint:wrapcheck // a test helper
	}
	b, err := mmdvm.AppendFrame(nil, d.Command(), p)
	if err != nil {
		return err //nolint:wrapcheck // a test helper
	}
	return f.Write(b)
}

func (f *Fake) read() {
	defer f.wg.Done()
	buf := make([]byte, 1024)
	var pending []byte
	for {
		n, err := f.conn.Read(buf)
		pending = append(pending, buf[:n]...)
		for len(pending) > 0 {
			fr, k, res := mmdvm.Next(pending)
			if res == mmdvm.NextMore {
				break
			}
			if res == mmdvm.NextFrame {
				fr.Payload = append([]byte(nil), fr.Payload...)
				f.handle(fr)
			}
			pending = pending[k:]
		}
		if err != nil {
			return
		}
	}
}

func (f *Fake) handle(fr mmdvm.Frame) {
	f.mu.Lock()
	f.got = append(f.got, fr)
	f.mu.Unlock()
	switch fr.Command { //nolint:exhaustive // the commands of the DMR firmware
	case mmdvm.CommandGetVersion:
		f.version()
	case mmdvm.CommandGetStatus:
		if !f.silent.Load() {
			f.status()
		}
	case mmdvm.CommandSetConfig:
		f.setConfig(fr.Payload)
	case mmdvm.CommandSetFreq:
		f.ack(fr.Command)
	case mmdvm.CommandSetMode:
		f.setMode(fr.Payload)
	case mmdvm.CommandDMRData1, mmdvm.CommandDMRData2:
		f.data(fr)
	case mmdvm.CommandDMRStart:
		f.start(fr.Payload)
	case mmdvm.CommandDMRShortLC:
		f.shortLC(fr.Payload)
	case mmdvm.CommandDMRAbort:
		f.abort(fr.Payload)
	default:
		f.nak(fr.Command, nakUnknown)
	}
	f.mu.Lock()
	f.notify()
	f.mu.Unlock()
}

func (f *Fake) version() {
	if f.protocol == 1 {
		f.reply(mmdvm.CommandGetVersion, append([]byte{1}, f.desc...))
		return
	}
	v := mmdvm.Version{Protocol: 2, Capabilities: mmdvm.CapDMR | mmdvm.CapDStar, CPU: mmdvm.CPUST, Description: []byte(f.desc)}
	p, err := v.AppendBinary(nil)
	if err == nil {
		f.reply(mmdvm.CommandGetVersion, p)
	}
}

func (f *Fake) space(s int) uint8 {
	return uint8(FIFOBursts - len(f.fifo[s])) //nolint:gosec // at most FIFOBursts
}

func (f *Fake) status() {
	f.mu.Lock()
	s1, s2 := f.space(0), f.space(1)
	if !f.duplex {
		s1 = 10
	}
	tx, mode := f.tx || !f.duplex && len(f.fifo[1]) > 0, f.mode
	f.mu.Unlock()
	var p []byte
	var err error
	if f.protocol == 1 {
		s := mmdvm.StatusV1{DMR: true, State: mode, TX: tx, DMRSpace1: s1, DMRSpace2: s2}
		p, err = s.AppendBinary(nil)
	} else {
		s := mmdvm.StatusV2{State: mode, TX: tx, DMRSpace1: s1, DMRSpace2: s2}
		p, err = s.AppendBinary(nil)
	}
	if err == nil {
		f.reply(mmdvm.CommandGetStatus, p)
	}
}

func (f *Fake) setConfig(p []byte) {
	if f.nakConfig.Load() {
		f.nak(mmdvm.CommandSetConfig, nakInvalid)
		return
	}
	var duplex, debug bool
	var cc enums.ColorCode
	var delay uint8
	if f.protocol == 1 {
		c, err := mmdvm.DecodeConfigV1(p)
		if err != nil || len(p) < configV1Len {
			f.nak(mmdvm.CommandSetConfig, nakInvalid)
			return
		}
		duplex, debug, cc, delay = !c.Simplex, c.Debug, c.DMRColorCode, c.TXDelay
	} else {
		c, err := mmdvm.DecodeConfigV2(p)
		if err != nil || len(p) < configV2Len {
			f.nak(mmdvm.CommandSetConfig, nakInvalid)
			return
		}
		duplex, debug, cc, delay = !c.Simplex, c.Debug, c.DMRColorCode, c.TXDelay
	}
	if delay > maxTXDelay || cc > maxColor {
		f.nak(mmdvm.CommandSetConfig, nakInvalid)
		return
	}
	f.mu.Lock()
	f.duplex, f.debug, f.cc, f.configs = duplex, debug, cc, f.configs+1
	f.mu.Unlock()
	f.ack(mmdvm.CommandSetConfig)
}

func (f *Fake) setMode(p []byte) {
	m, err := mmdvm.DecodeSetMode(p)
	if err != nil || m.Mode != mmdvm.ModeIdle && m.Mode != mmdvm.ModeDMR {
		f.nak(mmdvm.CommandSetMode, nakInvalid)
		return
	}
	f.mu.Lock()
	f.mode = m.Mode
	if m.Mode == mmdvm.ModeIdle {
		f.tx = false
	}
	f.mu.Unlock()
	f.ack(mmdvm.CommandSetMode)
}

func (f *Fake) data(fr mmdvm.Frame) {
	d, err := mmdvm.DecodeDMRData(fr)
	if err != nil || d.HasRSSI {
		f.nak(fr.Command, nakInvalid)
		return
	}
	i := int(d.Slot) - 1
	f.mu.Lock()
	reason := uint8(0)
	switch {
	case !f.duplex && d.Slot == enums.Slot1:
		reason = nakState
	case len(f.fifo[i]) >= FIFOBursts:
		reason = nakFull
	default:
		f.fifo[i] = append(f.fifo[i], d.Burst)
		if f.mode == mmdvm.ModeIdle {
			f.mode = mmdvm.ModeDMR
		}
	}
	f.mu.Unlock()
	if reason != 0 {
		f.nak(fr.Command, reason)
	}
}

func (f *Fake) start(p []byte) {
	s, err := mmdvm.DecodeDMRStart(p)
	f.mu.Lock()
	ok := err == nil && f.mode == mmdvm.ModeDMR
	if ok {
		f.tx = s.TX
	}
	f.mu.Unlock()
	if !ok {
		f.nak(mmdvm.CommandDMRStart, nakInvalid)
	}
}

func (f *Fake) shortLC(p []byte) {
	s, err := mmdvm.DecodeDMRShortLC(p)
	if err != nil {
		f.nak(mmdvm.CommandDMRShortLC, nakInvalid)
		return
	}
	f.mu.Lock()
	f.shortLCs = append(f.shortLCs, s)
	f.mu.Unlock()
}

func (f *Fake) abort(p []byte) {
	a, err := mmdvm.DecodeDMRAbort(p)
	if err != nil {
		f.nak(mmdvm.CommandDMRAbort, nakInvalid)
		return
	}
	f.mu.Lock()
	f.fifo[a.Slot-1] = nil
	f.mu.Unlock()
}

// transmit takes one burst off the FIFOs every 30 ms, slot 1 and slot 2 in
// turn, while the duplex transmitter is on; in simplex it sends a burst
// of the DMO FIFO every 60 ms.
func (f *Fake) transmit() {
	defer f.wg.Done()
	t := time.NewTicker(30 * time.Millisecond)
	defer t.Stop()
	slot := 0
	for {
		select {
		case <-f.done:
			return
		case <-t.C:
		}
		f.mu.Lock()
		on := f.duplex && f.tx || !f.duplex && slot == 1
		if on && len(f.fifo[slot]) > 0 {
			f.sent[slot] = append(f.sent[slot], f.fifo[slot][0])
			f.fifo[slot] = f.fifo[slot][1:]
			f.notify()
		}
		f.mu.Unlock()
		slot = 1 - slot
	}
}
