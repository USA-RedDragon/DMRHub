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
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DroidStarEnv names the environment variable that holds the path of a
// desktop DroidStar binary for the external client tests.
const DroidStarEnv = "DMRHUB_DROIDSTAR"

// ErrNotIsolated is returned by RequireIsolatedNetwork when the process can
// reach a network other than loopback.
var ErrNotIsolated = errors.New("the test can reach a network other than loopback")

// RequireIsolatedNetwork returns ErrNotIsolated unless every network
// interface of the process is a loopback interface, as in a network
// namespace made with unshare -r -n. DroidStar fetches host lists from
// public hosts when it starts, so it runs only there.
func RequireIsolatedNetwork() error {
	ifs, err := net.Interfaces()
	if err != nil {
		return fmt.Errorf("listing interfaces: %w", err)
	}
	for _, i := range ifs {
		if i.Flags&net.FlagLoopback == 0 {
			return fmt.Errorf("%w: interface %s", ErrNotIsolated, i.Name)
		}
	}
	return nil
}

// DroidStarConfig is the account and master that a DroidStar logs in with.
type DroidStarConfig struct {
	// Binary is the path of the DroidStar binary.
	Binary string
	// Home is the home directory that DroidStar keeps its settings in.
	Home string
	// Callsign and DMRID are the user. With ESSID 1 DroidStar logs in as
	// DMRID*100.
	Callsign string
	DMRID    uint32
	ESSID    uint8
	// Master is the host:port of the MMDVM server and Password the password
	// of the hotspot.
	Master   string
	Password string
	// Options is the RPTO text, such as "TS1=91;TS2=9;".
	Options string
	// TalkGroup is the talkgroup DroidStar selects.
	TalkGroup uint
}

// DroidStar runs a desktop DroidStar, nostar/DroidStar with the
// DROIDSTAR_E2E_AUTOCONNECT hook, as an MMDVM client of a test hub, and
// keeps its log.
type DroidStar struct {
	cmd    *exec.Cmd
	cancel context.CancelFunc
	done   chan struct{}

	mu    sync.Mutex
	lines []string
	wake  chan struct{}
	log   *os.File
}

// HotspotID returns the repeater ID that DroidStar logs in with.
func (c *DroidStarConfig) HotspotID() uint32 {
	if c.ESSID == 0 {
		return c.DMRID
	}
	return c.DMRID*100 + uint32(c.ESSID-1)
}

// StartDroidStar writes the settings of cfg to cfg.Home and starts
// DroidStar, which connects to the master after a second. Its output goes
// to cfg.Home/droidstar.log.
func StartDroidStar(cfg *DroidStarConfig) (*DroidStar, error) {
	host, port, err := net.SplitHostPort(cfg.Master)
	if err != nil {
		return nil, fmt.Errorf("master address: %w", err)
	}
	dir := filepath.Join(cfg.Home, ".config", "dudetronics")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("making settings directory: %w", err)
	}
	hosts := fmt.Sprintf("# Name ID Address Password Port\nDMRHUB 1 %s %s %s\n", host, cfg.Password, port)
	if err := os.WriteFile(filepath.Join(dir, "DMRHosts.txt"), []byte(hosts), 0o600); err != nil {
		return nil, fmt.Errorf("writing host list: %w", err)
	}
	ini := strings.Join([]string{
		"[General]",
		"MODE=DMR",
		"DMRHOST=DMRHUB",
		"CALLSIGN=" + cfg.Callsign,
		fmt.Sprintf("DMRID=%d", cfg.DMRID),
		fmt.Sprintf("ESSID=%d", cfg.ESSID),
		"BMPASSWORD=",
		"DMRLAT=30.2672",
		"DMRLONG=-97.7431",
		"DMRLOC=Austin, Texas, US",
		"DMRDESC=DroidStar test",
		"DMRFREQ=438800000",
		"DMRURL=https://example.org/droidstar",
		"DMROPTS=" + cfg.Options,
		fmt.Sprintf("DMRTGID=%d", cfg.TalkGroup),
		"TXTIMEOUT=300",
		"VOCODER=None",
		"MODEM=None",
		"IPV6=false",
		"",
	}, "\n")
	if err := os.WriteFile(filepath.Join(dir, "droidstar.ini"), []byte(ini), 0o600); err != nil {
		return nil, fmt.Errorf("writing settings: %w", err)
	}
	logFile, err := os.Create(filepath.Join(cfg.Home, "droidstar.log"))
	if err != nil {
		return nil, fmt.Errorf("making log: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cmd := exec.CommandContext(ctx, cfg.Binary)
	cmd.Env = append(os.Environ(),
		"HOME="+cfg.Home,
		"XDG_CONFIG_HOME="+filepath.Join(cfg.Home, ".config"),
		"QT_QPA_PLATFORM=offscreen",
		"QT_FORCE_STDERR_LOGGING=1",
		"QT_LOGGING_RULES=*.debug=true;qt.*.debug=false",
		"DROIDSTAR_E2E_AUTOCONNECT=1000",
	)
	out, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		_ = logFile.Close()
		return nil, fmt.Errorf("making output pipe: %w", err)
	}
	cmd.Stderr = cmd.Stdout
	if err := cmd.Start(); err != nil {
		cancel()
		_ = logFile.Close()
		return nil, fmt.Errorf("starting DroidStar: %w", err)
	}
	d := &DroidStar{cmd: cmd, cancel: cancel, done: make(chan struct{}), wake: make(chan struct{}), log: logFile}
	go d.read(out)
	return d, nil
}

func (d *DroidStar) read(r io.Reader) {
	defer close(d.done)
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		line := sc.Text()
		_, _ = fmt.Fprintf(d.log, "%s %s\n", time.Now().Format("15:04:05.000"), line)
		d.mu.Lock()
		d.lines = append(d.lines, line)
		close(d.wake)
		d.wake = make(chan struct{})
		d.mu.Unlock()
	}
}

// Lines returns the number of lines DroidStar has written.
func (d *DroidStar) Lines() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return len(d.lines)
}

// WaitLine waits until a line after the first from lines holds text, and
// returns its index.
func (d *DroidStar) WaitLine(from int, text string, timeout time.Duration) (int, bool) {
	deadline := time.After(timeout)
	for {
		d.mu.Lock()
		for i := from; i < len(d.lines); i++ {
			if strings.Contains(d.lines[i], text) {
				d.mu.Unlock()
				return i, true
			}
		}
		wake := d.wake
		d.mu.Unlock()
		select {
		case <-wake:
		case <-d.done:
			return -1, false
		case <-deadline:
			return -1, false
		}
	}
}

// Log returns everything DroidStar wrote.
func (d *DroidStar) Log() string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return strings.Join(d.lines, "\n")
}

// Stop stops DroidStar.
func (d *DroidStar) Stop() {
	d.cancel()
	_ = d.cmd.Wait()
	<-d.done
	_ = d.log.Close()
}
