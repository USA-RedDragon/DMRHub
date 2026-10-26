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

// streamClaimWait is the longest that the bursts of a new source of a
// stream wait for the KV store to answer for the claim of the stream.
const streamClaimWait = 250 * time.Millisecond

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

var (
	errNoClaimStore = errors.New("no KV store for stream claims")
	errClaimSlow    = errors.New("the KV store took too long")
)

// sourceSlot is a timeslot of a source of streams: a slot of a repeater,
// or an OpenBridge peer or a plugin on a slot. The hub keeps the order of
// the bursts of each across their streams.
type sourceSlot struct {
	streamSource
	slot bool
}

func sourceSlotOf(packet models.Packet, sourceName string) sourceSlot {
	return sourceSlot{streamSource: streamSource{server: sourceName, repeater: packet.Repeater}, slot: packet.Slot}
}

type heldBurst struct {
	packet models.Packet
	then   func(bool)
	ask    *claimAsk
}

// claimAsk is a check of the claim of a stream in the KV store. owner is
// set when done is closed.
type claimAsk struct {
	done  chan struct{}
	owner streamSource
}

// claimWait holds the bursts of a source slot while the hub checks the
// claims of streams of it in the KV store. The held bursts of a stream
// share one check.
type claimWait struct {
	mu   sync.Mutex
	held []heldBurst
	asks map[claimKey]*claimAsk
	done bool
}

// hold adds b to the held bursts of src, and starts a check of the claim
// of its stream when the hub does not know it. It returns false when w no
// longer holds bursts.
func (w *claimWait) hold(ctx context.Context, h *Hub, src streamSource, b heldBurst) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.done {
		return false
	}
	claim := claimKey{stream: b.packet.StreamID, src: b.packet.Src}
	if a, ok := w.asks[claim]; ok {
		b.ask = a
	} else if _, known := h.cachedOwner(ctx, b.packet, src); !known {
		b.ask = h.startAsk(ctx, claim, src)
		w.asks[claim] = b.ask
	}
	w.held = append(w.held, b)
	return true
}

// admitStream calls then with whether the hub takes packet in. The first
// source of a stream of a radio owns it until at least streamClaimHold
// passes with no burst from that source. The same stream from another
// source meanwhile is a loop or a second path to the same call, so it is
// dropped, counted and logged once per stream, as FreeDMR's LoopControl
// does. The claim is in the KV store, so it holds across replicas; a hub
// checks it once per streamClaimRecheck for each source of a stream, and a
// claim lasts streamClaimHold plus streamClaimRecheck after its owner's
// last check, so it outlasts the last burst by at least streamClaimHold. A
// repeater that reconnects or moves to another replica keeps its ID, so it
// keeps its stream.
//
// The caller never waits on the KV store. The owner of a stream keeps
// it while its claim is renewed there. When the hub has to check the claim
// of a stream from a source slot, it holds the bursts of that source slot,
// of any stream, until the checks of the streams of the bursts before
// them end, each after at most streamClaimWait, and then is called for
// them on another goroutine, in the order they came, so that the next
// stream of a source never overtakes the last one. The checks run at
// once, so a burst waits at most about streamClaimWait. Other source slots
// do not wait. A KV store that is slower than that or fails leaves the
// decision to this hub alone.
func (h *Hub) admitStream(ctx context.Context, packet models.Packet, sourceName string, then func(bool)) {
	key := sourceSlotOf(packet, sourceName)
	src := key.streamSource
	burst := heldBurst{packet: packet, then: then}
	for {
		if w, ok := h.claimWaits.Load(key); ok {
			if w.hold(ctx, h, src, burst) {
				return
			}
			continue
		}
		if owner, known := h.cachedOwner(ctx, packet, src); known {
			then(h.admitFrom(packet, src, owner))
			return
		}
		w := &claimWait{asks: map[claimKey]*claimAsk{}}
		if _, loaded := h.claimWaits.LoadOrStore(key, w); loaded {
			continue
		}
		w.hold(ctx, h, src, burst)
		go h.releaseHeld(ctx, key, w)
		return
	}
}

// cachedOwner returns the owner of the stream of packet from src, when the
// hub knows it without waiting on the KV store.
func (h *Hub) cachedOwner(ctx context.Context, packet models.Packet, src streamSource) (streamSource, bool) {
	if packet.StreamID == 0 {
		return src, true
	}
	claim := claimKey{stream: packet.StreamID, src: packet.Src}
	hold, recheck := h.claimTimes()
	cached, ok := h.streamClaims.Load(claim)
	now := time.Now()
	if ok && now.Sub(cached.checked) < recheck {
		return cached.source, true
	}
	if ok && cached.source == src && !cached.renewed.IsZero() && now.Sub(cached.renewed) < hold {
		h.streamClaims.Store(claim, streamClaim{source: src, checked: now, renewed: cached.renewed})
		go h.askClaim(ctx, claim, src, cached, ok)
		return src, true
	}
	return streamSource{}, false
}

func (h *Hub) startAsk(ctx context.Context, claim claimKey, src streamSource) *claimAsk {
	a := &claimAsk{done: make(chan struct{})}
	cached, known := h.streamClaims.Load(claim)
	go func() {
		a.owner = h.askClaim(ctx, claim, src, cached, known)
		close(a.done)
	}()
	return a
}

type claimResult struct {
	owner streamSource
	err   error
}

// releaseHeld calls then for the held bursts of key in order, each when
// the check of its stream ends, until none is held.
func (h *Hub) releaseHeld(ctx context.Context, key sourceSlot, w *claimWait) {
	src := key.streamSource
	for {
		w.mu.Lock()
		if len(w.held) == 0 {
			h.claimWaits.Delete(key)
			w.done = true
			w.mu.Unlock()
			return
		}
		b := w.held[0]
		w.held = w.held[1:]
		w.mu.Unlock()
		var owner streamSource
		if b.ask != nil {
			<-b.ask.done
			owner = b.ask.owner
		} else if o, known := h.cachedOwner(ctx, b.packet, src); known {
			owner = o
		} else {
			a := h.startAsk(ctx, claimKey{stream: b.packet.StreamID, src: b.packet.Src}, src)
			<-a.done
			owner = a.owner
		}
		b.then(h.admitFrom(b.packet, src, owner))
	}
}

// askClaim takes or renews the claim of src in the KV store, waiting for
// at most streamClaimWait, stores the answer and returns the owner. cached
// and known are the claim that the hub knew before.
func (h *Hub) askClaim(ctx context.Context, claim claimKey, src streamSource, cached streamClaim, known bool) streamSource {
	hold, recheck := h.claimTimes()
	owned := known && cached.source == src && !cached.renewed.IsZero() && time.Since(cached.renewed) < hold
	ctx, cancel := context.WithTimeout(ctx, streamClaimWait)
	defer cancel()
	answer := make(chan claimResult, 1)
	go func() {
		owner, err := h.sharedClaim(ctx, claim, src, owned, hold+recheck)
		answer <- claimResult{owner: owner, err: err}
	}()
	timer := time.NewTimer(streamClaimWait)
	defer timer.Stop()
	var r claimResult
	select {
	case r = <-answer:
	case <-timer.C:
		r.err = errClaimSlow
	}

	now := time.Now()
	owner := cached.source
	switch {
	case r.err == nil && r.owner == src:
		owner = src
		h.streamClaims.Store(claim, streamClaim{source: src, checked: now, renewed: now})
	case r.err == nil:
		owner = r.owner
		h.streamClaims.Store(claim, streamClaim{source: r.owner, checked: now})
	case !known || cached.source == src || now.Sub(cached.checked) > hold+recheck:
		owner = src
		h.streamClaims.Store(claim, streamClaim{source: src, checked: now, renewed: now})
	}
	if r.err != nil && !errors.Is(r.err, errNoClaimStore) {
		slog.Warn("Failed to check the shared claim of a stream", "stream", claim.stream, "src", claim.src, "error", r.err)
	}
	return owner
}

// admitFrom reports whether owner is src, and counts and logs packet as a
// dropped copy when it is not.
func (h *Hub) admitFrom(packet models.Packet, src, owner streamSource) bool {
	if owner == src {
		return true
	}
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonStreamFromOtherSource)
	if _, logged := h.loopLogged.LoadOrStore(packet.StreamID, time.Now()); !logged {
		slog.Warn("Dropping a stream that another source is already sending",
			"stream", packet.StreamID, "src", packet.Src, "dst", packet.Dst,
			"server", src.server, "repeaterID", packet.Repeater,
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
