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
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem/modemtest"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
	"golang.org/x/sys/unix"
)

// openPTY returns the master of a new pseudo terminal and the path of its
// slave, or skips the test when the system has none for it. It holds the
// slave open until the test ends: a read of the master fails with EIO
// while no slave is open, which would stop a fake modem when a test
// closes the port, where a board keeps running.
func openPTY(t *testing.T) (*os.File, string) {
	t.Helper()
	fd, err := unix.Open("/dev/ptmx", unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		t.Skipf("no pseudo terminal: %v", err)
	}
	if err := unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0); err != nil {
		_ = unix.Close(fd)
		t.Skipf("cannot unlock the pseudo terminal: %v", err)
	}
	n, err := unix.IoctlGetInt(fd, unix.TIOCGPTN)
	if err != nil {
		_ = unix.Close(fd)
		t.Skipf("no pseudo terminal number: %v", err)
	}
	slave := "/dev/pts/" + strconv.Itoa(n)
	hold, err := unix.Open(slave, unix.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
	if err != nil {
		_ = unix.Close(fd)
		t.Skipf("cannot open the pseudo terminal: %v", err)
	}
	t.Cleanup(func() { _ = unix.Close(hold) })
	return os.NewFile(uintptr(fd), "/dev/ptmx"), slave
}

// TestServerOverAPseudoTerminal runs the server through OpenSerial, the
// termios setup of a real port, against the fake modem on the master side.
func TestServerOverAPseudoTerminal(t *testing.T) {
	t.Parallel()
	master, slave := openPTY(t)
	m := modemtest.New(master, 2)
	t.Cleanup(m.Close)
	cfg := modemConfig(t, true)
	cfg.DMR.Modem.Port = slave
	opened := make(chan struct{}, 1)
	s := modem.NewServerWithOptions(cfg, nil, nil, "test", modem.Options{
		Open: func(path string, baud int) (modem.Port, error) {
			p, err := modem.OpenSerial(path, baud)
			if err != nil {
				return nil, fmt.Errorf("open the pseudo terminal: %w", err)
			}
			opened <- struct{}{}
			return p, nil
		},
		Reply: 500 * time.Millisecond, Reopen: time.Hour, Retry: time.Hour,
	})
	if err := s.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Stop(context.Background()) })
	select {
	case <-opened:
	case <-time.After(waitLink):
		t.Fatal("OpenSerial failed on the pseudo terminal")
	}
	if !m.WaitFor(waitLink, polled(2)) || !s.Linked() {
		t.Fatal("no link over the pseudo terminal")
	}
	if !m.WaitFor(0, func(f *modemtest.Fake) bool { return f.Duplex() && f.Configured() == 1 }) {
		t.Fatal("the modem did not take one duplex config")
	}
	if err := s.Stop(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !m.WaitFor(waitLink, func(f *modemtest.Fake) bool { return f.Mode() == mmdvm.ModeIdle && f.Count(mmdvm.CommandSetMode) >= 2 }) {
		t.Fatal("no idle mode on stop")
	}
}

func TestOpenSerialRefusesABadSpeed(t *testing.T) {
	t.Parallel()
	if _, err := modem.OpenSerial("/dev/null", 1200); err == nil {
		t.Fatal("1200 baud opened")
	}
}
