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

package hub

import (
	"sync"
	"time"
)

// streamIdle is the time after the last packet of a stream at which the
// stream counts as ended, for a stream that ends with no terminator.
const streamIdle = 2 * time.Second

// inflight counts work that shutdown must wait for. Unlike a
// sync.WaitGroup, Add may run while Wait waits.
type inflight struct {
	mu   sync.Mutex
	n    int
	zero chan struct{}
}

func (f *inflight) Add() {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.n == 0 {
		f.zero = make(chan struct{})
	}
	f.n++
}

func (f *inflight) Done() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.n--
	if f.n == 0 {
		close(f.zero)
	}
}

// Wait blocks until no work is in flight.
func (f *inflight) Wait() {
	f.mu.Lock()
	if f.n == 0 {
		f.mu.Unlock()
		return
	}
	zero := f.zero
	f.mu.Unlock()
	<-zero
}

type activeStream struct {
	timer *time.Timer
}

// streamActive counts the stream of a routed packet as in progress until
// streamIdle passes with no packet of it, or streamEnded.
func (h *Hub) streamActive(stream uint) {
	h.streamsMu.Lock()
	defer h.streamsMu.Unlock()
	if s, ok := h.activeStreams[stream]; ok {
		s.timer.Reset(streamIdle)
		return
	}
	h.calls.Add()
	s := &activeStream{}
	s.timer = time.AfterFunc(streamIdle, func() { h.endStream(stream, s) })
	h.activeStreams[stream] = s
}

// streamEnded ends a stream at its terminator.
func (h *Hub) streamEnded(stream uint) {
	h.streamsMu.Lock()
	s, ok := h.activeStreams[stream]
	if ok {
		s.timer.Stop()
	}
	h.streamsMu.Unlock()
	if ok {
		h.endStream(stream, s)
	}
}

func (h *Hub) endStream(stream uint, s *activeStream) {
	h.streamsMu.Lock()
	defer h.streamsMu.Unlock()
	if h.activeStreams[stream] != s {
		return
	}
	delete(h.activeStreams, stream)
	h.calls.Done()
}
