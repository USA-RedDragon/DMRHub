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

package modem

import (
	"context"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/dmrgo/v2/transport/mmdvm"
)

// Step is the answer of the board to one command of a Probe.
type Step struct {
	Taken bool   `json:"taken"`
	Error string `json:"error,omitempty"`
}

// ProbeResult is what a Probe found.
type ProbeResult struct {
	Port string `json:"port"`
	Baud int    `json:"baud"`
	// Opened is whether the serial port opened.
	Opened bool `json:"opened"`
	// Tries is how many times the probe asked the board for its version.
	Tries int `json:"tries"`
	// Answered is whether the board sent its version.
	Answered bool     `json:"answered"`
	Firmware string   `json:"firmware,omitempty"`
	Protocol uint8    `json:"protocol,omitempty"`
	Modes    []string `json:"modes,omitempty"`
	// Frequencies is the answer to the frequencies, nil when the probe
	// did not send them.
	Frequencies *Step `json:"frequencies,omitempty"`
	// Settings is the answer to the config, nil when the probe did not
	// send it.
	Settings *Step `json:"settings,omitempty"`
	// Error is why the probe stopped, empty when the board took
	// everything.
	Error string `json:"error,omitempty"`
}

// Probe opens the serial port of m with the open sequence of the server:
// it asks the board for its version, sends the frequencies and the config,
// puts the board in idle mode and closes the port. It saves nothing and
// stops when ctx ends. Each reply waits opts.Reply, after opts.OpenDelay.
func Probe(ctx context.Context, m config.Modem, opts Options) ProbeResult {
	if opts.Open == nil {
		opts.Open = OpenSerial
	}
	r := ProbeResult{Port: m.Port, Baud: m.Baud}
	s := &Server{m: m, opts: opts, done: make(chan struct{})}
	stop := context.AfterFunc(ctx, func() { close(s.done) })
	defer stop()
	p, err := opts.Open(m.Port, m.Baud)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Opened = true
	l := newLink(p)
	defer l.finish(finishTimeout)
	if !s.sleep(opts.OpenDelay) {
		r.Error = errStopped.Error()
		return r
	}
	v, tries, err := s.askVersion(l)
	r.Tries = tries
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Answered, r.Firmware, r.Protocol, r.Modes = true, firmware(v), v.Protocol, Modes(v.Capabilities)
	if err := checkVersion(v); err != nil {
		r.Error = err.Error()
		return r
	}
	freq := setFreq(&s.m)
	r.Frequencies = step(s.command(l, mmdvm.CommandSetFreq, &freq))
	if !r.Frequencies.Taken {
		r.Error = r.Frequencies.Error
		return r
	}
	cfg, err := configFrame(&s.m, v.Protocol)
	if err != nil {
		r.Error = err.Error()
		return r
	}
	r.Settings = step(s.await(l, mmdvm.CommandSetConfig, cfg))
	if !r.Settings.Taken {
		r.Error = r.Settings.Error
		return r
	}
	if idle, err := modeFrame(mmdvm.ModeIdle); err == nil {
		l.send(idle)
	}
	return r
}

func step(err error) *Step {
	if err != nil {
		return &Step{Error: err.Error()}
	}
	return &Step{Taken: true}
}
