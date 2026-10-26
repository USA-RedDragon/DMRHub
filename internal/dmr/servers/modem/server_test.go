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

package modem_test

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

const (
	waitLink = 5 * time.Second
	testID   = 311860
)

func modemConfig(t *testing.T, duplex bool) *config.Config {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	if err != nil {
		t.Fatal(err)
	}
	m := &cfg.DMR.Modem
	m.Enabled, m.Port, m.RepeaterID, m.Duplex = true, "/dev/fake-modem", testID, duplex
	m.RXFrequency, m.TXFrequency = 438800000, 438800000
	return &cfg
}

// fakes is an Opener that puts a fake modem on the other end of each port
// it opens, and hands the fakes to the test.
type fakes struct {
	protocol uint8
	setup    func(n int, f *modemtest.Fake)
	opened   chan *modemtest.Fake
	n        int
}

func newFakes(t *testing.T, protocol uint8) *fakes {
	t.Helper()
	f := &fakes{protocol: protocol, opened: make(chan *modemtest.Fake, 16)}
	t.Cleanup(func() {
		for {
			select {
			case m := <-f.opened:
				m.Close()
			default:
				return
			}
		}
	})
	return f
}

func (f *fakes) open(string, int) (modem.Port, error) {
	host, dev := net.Pipe()
	m := modemtest.New(dev, f.protocol)
	if f.setup != nil {
		f.setup(f.n, m)
	}
	f.n++
	f.opened <- m
	return host, nil
}

func (f *fakes) next(t *testing.T) *modemtest.Fake {
	t.Helper()
	select {
	case m := <-f.opened:
		t.Cleanup(m.Close)
		return m
	case <-time.After(waitLink):
		t.Fatal("the server did not open the modem")
		return nil
	}
}

func startServer(t *testing.T, cfg *config.Config, f *fakes) *modem.Server {
	t.Helper()
	s := modem.NewServerWithOptions(cfg, nil, nil, "test", modem.Options{Open: f.open, Reply: 200 * time.Millisecond,
		Reopen: 50 * time.Millisecond, Retry: 50 * time.Millisecond})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	return s
}

func polled(n int) func(f *modemtest.Fake) bool {
	return func(f *modemtest.Fake) bool { return f.Count(mmdvm.CommandGetStatus) >= n }
}

func TestServerOpensTheModemAsMMDVMHost(t *testing.T) {
	t.Parallel()
	for _, protocol := range []uint8{1, 2} {
		f := newFakes(t, protocol)
		s := startServer(t, modemConfig(t, false), f)
		m := f.next(t)
		if !m.WaitFor(waitLink, polled(2)) {
			t.Fatalf("protocol %d: no status requests", protocol)
		}
		if !s.Linked() {
			t.Fatalf("protocol %d: not linked", protocol)
		}
		var got []mmdvm.Command
		m.WaitFor(0, func(f *modemtest.Fake) bool {
			for _, fr := range f.Frames()[:5] {
				got = append(got, fr.Command)
			}
			return true
		})
		want := []mmdvm.Command{mmdvm.CommandGetVersion, mmdvm.CommandSetFreq, mmdvm.CommandSetConfig, mmdvm.CommandSetMode, mmdvm.CommandGetStatus}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("protocol %d: the server sent %v, want %v", protocol, got, want)
			}
		}
		if s.Addr().String() != "/dev/fake-modem" || s.Addr().Network() != "serial" {
			t.Fatalf("address %v", s.Addr())
		}
	}
}

func TestServerTriesAgainAfterTheModemRefusesTheConfig(t *testing.T) {
	t.Parallel()
	f := newFakes(t, 2)
	f.setup = func(n int, m *modemtest.Fake) { m.SetNAKConfig(n < 2) }
	s := startServer(t, modemConfig(t, true), f)
	for range 2 {
		m := f.next(t)
		if !m.WaitFor(waitLink, func(f *modemtest.Fake) bool { return f.Count(mmdvm.CommandSetConfig) == 1 }) {
			t.Fatal("no SET_CONFIG")
		}
	}
	m := f.next(t)
	if !m.WaitFor(waitLink, polled(2)) || !s.Linked() {
		t.Fatal("the server did not link the modem that took the config")
	}
	if !m.WaitFor(0, func(f *modemtest.Fake) bool { return f.Duplex() && f.Configured() == 1 }) {
		t.Fatal("the modem did not take one duplex config")
	}
}

func TestServerOpensASilentModemAgain(t *testing.T) {
	t.Parallel()
	f := newFakes(t, 1)
	s := startServer(t, modemConfig(t, false), f)
	m := f.next(t)
	if !m.WaitFor(waitLink, polled(2)) {
		t.Fatal("no status requests")
	}
	m.SetSilent(true)
	start := time.Now()
	again := f.next(t)
	if d := time.Since(start); d < 1500*time.Millisecond {
		t.Fatalf("the server gave up on the modem after %s", d)
	}
	if !again.WaitFor(waitLink, polled(2)) || !s.Linked() {
		t.Fatal("the server did not link the modem again")
	}
}

func TestServerStopTurnsTheTransmitterOff(t *testing.T) {
	t.Parallel()
	f := newFakes(t, 2)
	s := startServer(t, modemConfig(t, true), f)
	m := f.next(t)
	if !m.WaitFor(waitLink, polled(2)) {
		t.Fatal("no status requests")
	}
	before := 0
	m.WaitFor(0, func(f *modemtest.Fake) bool { before = f.Count(mmdvm.CommandDMRStart); return true })
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !m.WaitFor(waitLink, func(f *modemtest.Fake) bool {
		fr := f.Frames()
		return f.Count(mmdvm.CommandDMRStart) == before+1 && fr[len(fr)-1].Command == mmdvm.CommandSetMode &&
			fr[len(fr)-1].Payload[0] == byte(mmdvm.ModeIdle)
	}) {
		t.Fatal("the server did not send DMR_START 0 and idle on stop")
	}
	if s.Linked() {
		t.Fatal("linked after stop")
	}
}

func TestServerSkipsGarbageFromTheModem(t *testing.T) {
	t.Parallel()
	f := newFakes(t, 2)
	s := startServer(t, modemConfig(t, false), f)
	m := f.next(t)
	if !m.WaitFor(waitLink, polled(2)) {
		t.Fatal("no status requests")
	}
	for _, g := range [][]byte{{0x00, 0x01, 0xE0}, {0x02, 0xE0, 0x00, 0x00}, {0xE0, 0x00, 0xFF}, make([]byte, 600), {0xE0, 0x05, 0x7F, 0x02, 0x04}} {
		if err := m.Write(g); err != nil {
			t.Fatal(err)
		}
	}
	n := 0
	m.WaitFor(0, func(f *modemtest.Fake) bool { n = f.Count(mmdvm.CommandGetStatus); return true })
	if !m.WaitFor(waitLink, polled(n+4)) || !s.Linked() {
		t.Fatal("the server stopped polling after garbage")
	}
}
