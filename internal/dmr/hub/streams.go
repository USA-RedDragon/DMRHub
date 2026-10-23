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
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/puzpuzpuz/xsync/v4"
)

// streamIdle is the time after the last packet of a stream at which the
// stream counts as ended, for a stream that ends with no terminator.
const streamIdle = 2 * time.Second

// streamClaimHold is how long a stream stays with its source after the
// last burst from it, so that copies looping back after the terminator are
// still dropped.
const streamClaimHold = 30 * time.Second

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

// streamSource is where the bursts of a stream come in: a server and the
// repeater or OpenBridge peer on it.
type streamSource struct {
	server   string
	repeater uint
}

// claimKey is a stream of one radio. Some clients reuse stream IDs, such as
// DroidStar, which calls rand() without srand(), so every instance sends the
// same sequence; a loop keeps the source radio.
type claimKey struct {
	stream, src uint
}

type streamClaim struct {
	source streamSource
	last   time.Time
}

// AdmitStream reports whether the hub takes packet in. The first source of
// a stream of a radio owns it until streamClaimHold passes with no burst from that
// source. The same stream from another source meanwhile is a loop or a
// second path to the same call, so it is dropped, counted and logged once
// per stream, as FreeDMR's LoopControl does. A repeater that reconnects or
// moves to another replica keeps its ID, so it keeps its stream.
func (h *Hub) AdmitStream(packet models.Packet, sourceName string) bool {
	if packet.StreamID == 0 {
		return true
	}
	src := streamSource{server: sourceName, repeater: packet.Repeater}
	now := time.Now()
	var owner streamSource
	admitted := true
	h.streamClaims.Compute(claimKey{stream: packet.StreamID, src: packet.Src}, func(old streamClaim, loaded bool) (streamClaim, xsync.ComputeOp) {
		if loaded && old.source != src && now.Sub(old.last) <= streamClaimHold {
			owner = old.source
			admitted = false
			return old, xsync.CancelOp
		}
		return streamClaim{source: src, last: now}, xsync.UpdateOp
	})
	if admitted {
		return true
	}
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonStreamFromOtherSource)
	if _, logged := h.loopLogged.LoadOrStore(packet.StreamID, now); !logged {
		slog.Warn("Dropping a stream that another source is already sending",
			"stream", packet.StreamID, "src", packet.Src, "dst", packet.Dst,
			"server", sourceName, "repeaterID", packet.Repeater,
			"ownerServer", owner.server, "ownerRepeaterID", owner.repeater)
	}
	return false
}

func (h *Hub) pruneStreamClaims(now time.Time) {
	h.streamClaims.Range(func(k claimKey, v streamClaim) bool {
		if now.Sub(v.last) > streamClaimHold {
			h.streamClaims.Delete(k)
		}
		return true
	})
}
