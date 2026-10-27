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

//go:build linux

package modem_test

import (
	"context"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"golang.org/x/sys/unix"
)

const probeReply = 200 * time.Millisecond

func probeOptions() modem.Options {
	return modem.Options{Open: modem.OpenSerial, Reply: probeReply}
}

// uart is the master of a pseudo terminal as the UART of a board that
// runs at one speed: while the host side runs at another, each octet
// comes out garbled both ways, as on a real port.
type uart struct {
	*os.File
	speed uint32
}

func (u uart) match() bool {
	ok := false
	c, err := u.SyscallConn()
	if err != nil {
		return false
	}
	_ = c.Control(func(fd uintptr) {
		t, err := unix.IoctlGetTermios(int(fd), unix.TCGETS) //nolint:gosec // a file descriptor
		ok = err == nil && t.Cflag&unix.CBAUD == u.speed
	})
	return ok
}

func garble(p []byte) {
	for i := range p {
		p[i] = ^p[i]
	}
}

func (u uart) Read(p []byte) (int, error) {
	n, err := u.File.Read(p)
	if !u.match() {
		garble(p[:n])
	}
	return n, err //nolint:wrapcheck // the error of the port
}

func (u uart) Write(p []byte) (int, error) {
	if !u.match() {
		p = slices.Clone(p)
		garble(p)
	}
	return u.File.Write(p) //nolint:wrapcheck // the error of the port
}

func TestProbeReadsTheBoard(t *testing.T) {
	t.Parallel()
	for _, protocol := range []uint8{1, 2} {
		master, slave := openPTY(t)
		m := modemtest.New(master, protocol)
		t.Cleanup(m.Close)
		cfg := modemConfig(t, false).DMR.Modem
		cfg.Port = slave
		r := modem.Probe(context.Background(), cfg, probeOptions())
		if !r.Opened || !r.Answered || r.Error != "" || r.Tries != 1 || r.Port != slave || r.Baud != 115200 {
			t.Fatalf("protocol %d: %+v", protocol, r)
		}
		if r.Firmware != "MMDVM fake 20261026" || r.Protocol != protocol || r.Modes[0] != "DMR" {
			t.Fatalf("protocol %d: the board is %+v", protocol, r)
		}
		if r.Frequencies == nil || !r.Frequencies.Taken || r.Settings == nil || !r.Settings.Taken {
			t.Fatalf("protocol %d: the board did not take the settings: %+v", protocol, r)
		}
		if !m.WaitFor(waitLink, func(f *modemtest.Fake) bool { return f.Configured() == 1 && f.Count(mmdvm.CommandSetMode) == 1 }) {
			t.Fatalf("protocol %d: the fake took no config and idle mode", protocol)
		}
	}
}

func TestProbeShowsARefusedConfig(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	m := modemtest.New(master, 2)
	t.Cleanup(m.Close)
	m.SetNAKConfig(true)
	cfg := modemConfig(t, true).DMR.Modem
	cfg.Port = slave
	r := modem.Probe(context.Background(), cfg, probeOptions())
	if !r.Answered || !r.Frequencies.Taken || r.Settings == nil || r.Settings.Taken || !strings.Contains(r.Error, "refused") {
		t.Fatalf("%+v", r)
	}
}

func TestProbeOfABoardThatDoesNotAnswer(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	t.Cleanup(func() { _ = master.Close() })
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port = slave
	start := time.Now()
	r := modem.Probe(context.Background(), cfg, probeOptions())
	if !r.Opened || r.Answered || r.Tries != 6 || !strings.Contains(r.Error, "no version reply") || r.Frequencies != nil {
		t.Fatalf("%+v", r)
	}
	if d := time.Since(start); d > 6*probeReply+time.Second {
		t.Fatalf("the probe took %v", d)
	}
}

func TestProbeAtTheWrongSpeed(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	m := modemtest.New(uart{File: master, speed: unix.B115200}, 2)
	t.Cleanup(m.Close)
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port, cfg.Baud = slave, 9600
	r := modem.Probe(context.Background(), cfg, probeOptions())
	if !r.Opened || r.Answered || r.Tries != 6 || r.Baud != 9600 {
		t.Fatalf("at 9600: %+v", r)
	}
	cfg.Baud = 115200
	if r := modem.Probe(context.Background(), cfg, probeOptions()); !r.Answered || !r.Settings.Taken {
		t.Fatalf("at 115200: %+v", r)
	}
}

func TestProbeOfAPortThatDoesNotOpen(t *testing.T) {
	t.Parallel()
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port = "/dev/no-such-modem"
	r := modem.Probe(context.Background(), cfg, probeOptions())
	if r.Opened || r.Tries != 0 || !strings.Contains(r.Error, "no such file") {
		t.Fatalf("%+v", r)
	}
}

func TestProbeStopsWithItsContext(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	t.Cleanup(func() { _ = master.Close() })
	cfg := modemConfig(t, false).DMR.Modem
	cfg.Port = slave
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	opts := probeOptions()
	opts.Reply = time.Minute
	r := modem.Probe(ctx, cfg, opts)
	if r.Answered || time.Since(start) > 5*time.Second {
		t.Fatalf("%+v after %v", r, time.Since(start))
	}
}
