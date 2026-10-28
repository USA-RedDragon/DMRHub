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
	"fmt"
	"sync"
	"testing"
	"time"
)

const starvationStep = 5 * time.Millisecond

// StarvationProbe measures how starved of CPU the process is while a test
// runs. A goroutine sleeps in steps of 5 ms and records how late each step
// wakes up. A test that runs on real time, such as one that paces a call,
// can use it to tell a loss that the code under test causes from one that
// a machine too loaded to run the test on time causes.
type StarvationProbe struct {
	stop   chan struct{}
	done   chan struct{}
	once   sync.Once
	report StarvationReport
}

// StarvationReport is what a StarvationProbe measured.
type StarvationReport struct {
	// Window is how long the probe ran.
	Window time.Duration
	// Worst is the latest that one step woke up.
	Worst time.Duration
	// Late is the sum of how late each step woke up.
	Late time.Duration
}

// Starved returns the share of the window that the steps woke up late,
// from 0 to 1.
func (r StarvationReport) Starved() float64 {
	if r.Window <= 0 {
		return 0
	}
	return float64(r.Late) / float64(r.Window)
}

// String gives the measurements as one line for a log.
func (r StarvationReport) String() string {
	return fmt.Sprintf("%.0f%% of %s late, worst step %s late", 100*r.Starved(), r.Window.Round(time.Millisecond), r.Worst.Round(time.Millisecond))
}

// StartStarvationProbe starts a StarvationProbe. It stops at the cleanup
// of t if Stop was not called first.
func StartStarvationProbe(t testing.TB) *StarvationProbe {
	t.Helper()
	p := &StarvationProbe{stop: make(chan struct{}), done: make(chan struct{})}
	go p.run()
	t.Cleanup(func() { p.Stop() })
	return p
}

func (p *StarvationProbe) run() {
	defer close(p.done)
	start := time.Now()
	prev := start
	for {
		select {
		case <-p.stop:
			p.report.Window = time.Since(start)
			return
		default:
		}
		time.Sleep(starvationStep)
		now := time.Now()
		late := now.Sub(prev) - starvationStep
		prev = now
		if late <= 0 {
			continue
		}
		p.report.Late += late
		if late > p.report.Worst {
			p.report.Worst = late
		}
	}
}

// Stop stops the probe and returns what it measured. Later calls return
// the same report.
func (p *StarvationProbe) Stop() StarvationReport {
	p.once.Do(func() { close(p.stop) })
	<-p.done
	return p.report
}

// SkipIfStarved stops the probe, logs what it measured, and skips the rest
// of t with that reason when the steps woke up late for more than the share
// bound of the window: the machine did not run the test on time, so a check
// that holds only on real time cannot hold.
func (p *StarvationProbe) SkipIfStarved(t testing.TB, bound float64) {
	t.Helper()
	r := p.Stop()
	if r.Starved() > bound {
		t.Skipf("the machine starved the test of CPU past the bound of %.0f%% late: %s", 100*bound, r)
	}
	t.Logf("the machine ran the test on time: %s", r)
}
