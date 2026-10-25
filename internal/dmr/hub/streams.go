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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
)

// streamIdle is the time after the last packet of a stream at which the
// stream counts as ended, for a stream that ends with no terminator.
const streamIdle = 2 * time.Second

// streamClaimHold is how long a stream stays with its source after the
// last burst from it, so that copies looping back after the terminator are
// still dropped.
const streamClaimHold = 30 * time.Second

const streamClaimRecheck = 5 * time.Second

const streamClaimKeyPrefix = "dmrhub:stream_claim:"

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

func (s streamSource) String() string {
	return s.server + "/" + strconv.FormatUint(uint64(s.repeater), 10)
}

func parseStreamSource(v string) streamSource {
	i := strings.LastIndexByte(v, '/')
	if i < 0 {
		return streamSource{server: v}
	}
	id, _ := strconv.ParseUint(v[i+1:], 10, 64)
	return streamSource{server: v[:i], repeater: uint(id)}
}

// claimKey is a stream of one radio. Some clients reuse stream IDs, such as
// DroidStar, which calls rand() without srand(), so every instance sends the
// same sequence; a loop keeps the source radio.
type claimKey struct {
	stream, src uint
}

func (k claimKey) kvKey() string {
	return streamClaimKeyPrefix + strconv.FormatUint(uint64(k.stream), 10) + ":" + strconv.FormatUint(uint64(k.src), 10)
}

type streamClaim struct {
	source  streamSource
	checked time.Time
	renewed time.Time
}

var errNoClaimStore = errors.New("no KV store for stream claims")

// AdmitStream reports whether the hub takes packet in. The first source of
// a stream of a radio owns it until at least streamClaimHold passes with no
// burst from that source. The same stream from another source meanwhile is
// a loop or a second path to the same call, so it is dropped, counted and
// logged once per stream, as FreeDMR's LoopControl does. The claim is in
// the KV store, so it holds across replicas; a hub checks it once per
// streamClaimRecheck for each source of a stream, and a claim lasts
// streamClaimHold plus streamClaimRecheck after its owner's last check, so
// it outlasts the last burst by at least streamClaimHold. A repeater that
// reconnects or moves to another replica keeps its ID, so it keeps its
// stream.
func (h *Hub) AdmitStream(ctx context.Context, packet models.Packet, sourceName string) bool {
	if packet.StreamID == 0 {
		return true
	}
	src := streamSource{server: sourceName, repeater: packet.Repeater}
	key := claimKey{stream: packet.StreamID, src: packet.Src}
	now := time.Now()
	hold, recheck := h.claimTimes()
	cached, known := h.streamClaims.Load(key)
	owner := cached.source
	if !known || now.Sub(cached.checked) >= recheck {
		owned := known && cached.source == src && !cached.renewed.IsZero() && now.Sub(cached.renewed) < hold
		shared, err := h.sharedClaim(ctx, key, src, owned, hold+recheck)
		switch {
		case err == nil && shared == src:
			owner = src
			h.streamClaims.Store(key, streamClaim{source: src, checked: now, renewed: now})
		case err == nil:
			owner = shared
			h.streamClaims.Store(key, streamClaim{source: shared, checked: now})
		case !known || cached.source == src || now.Sub(cached.checked) > hold+recheck:
			owner = src
			h.streamClaims.Store(key, streamClaim{source: src, checked: now, renewed: now})
		}
		if err != nil && !errors.Is(err, errNoClaimStore) {
			slog.Warn("Failed to check the shared claim of a stream", "stream", packet.StreamID, "src", packet.Src, "error", err)
		}
	}
	if owner == src {
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

func (h *Hub) sharedClaim(ctx context.Context, key claimKey, src streamSource, owned bool, ttl time.Duration) (streamSource, error) {
	if h.kv == nil {
		return streamSource{}, errNoClaimStore
	}
	k := key.kvKey()
	if owned && h.kv.Expire(ctx, k, ttl) == nil {
		return src, nil
	}
	for range 2 {
		set, err := h.kv.SetNX(ctx, k, src.String(), ttl)
		if err != nil {
			return streamSource{}, fmt.Errorf("claim stream: %w", err)
		}
		if set {
			return src, nil
		}
		v, err := h.kv.Get(ctx, k)
		if err != nil {
			continue
		}
		owner := parseStreamSource(string(v))
		if owner == src && h.kv.Expire(ctx, k, ttl) != nil {
			continue
		}
		return owner, nil
	}
	return streamSource{}, errors.New("claim stream: the claim expired while it was read")
}

func (h *Hub) claimTimes() (hold, recheck time.Duration) {
	if t := h.claimTiming.Load(); t != nil {
		return t[0], t[1]
	}
	return streamClaimHold, streamClaimRecheck
}

func (h *Hub) pruneStreamClaims(now time.Time) {
	hold, recheck := h.claimTimes()
	h.streamClaims.Range(func(k claimKey, v streamClaim) bool {
		if now.Sub(v.checked) > hold+recheck {
			h.streamClaims.Delete(k)
		}
		return true
	})
}
