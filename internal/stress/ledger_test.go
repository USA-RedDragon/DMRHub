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

//go:build stress

package stress_test

import (
	"fmt"
	"hash/fnv"
	"sort"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

// callRec is one scripted transmission and every delivery of its bursts.
type callRec struct {
	stream   uint
	kind     string
	label    string
	src, dst uint
	group    bool
	slot2    bool
	ingress  uint
	rowKind  string
	text     string
	sub      string
	confirm  bool
	want     []uint
	wantIdx  map[uint]int
	byseq    [256]int32
	voiceN   int
	byLoad   map[uint32]int
	lcBurst  []bool
	loose    map[uint]bool
	foreign  bool
	shed     bool
	optional map[uint]bool
	copies   []atomic.Int32
	sentAt   []int64
	hash     []uint32
	got      [][]uint8
	extra    map[uint]int
	altered  int
	sent     atomic.Int32
	lastSent atomic.Int64
	done     atomic.Bool
	mu       sync.Mutex
}

// ledger knows every burst that the generator sent and the targets that
// must get it, and counts what each target got.
type ledger struct {
	epoch  time.Time
	shards [64]struct {
		mu    sync.RWMutex
		calls map[uint]*callRec
	}
	lat     map[string]*histogram
	latAll  *histogram
	stray   atomic.Int64
	badKey  atomic.Int64
	recv    atomic.Int64
	sent    atomic.Int64
	loose   atomic.Int64
	bySD    sync.Map
	byCall  sync.Map
	parrot  sync.Map
	totals  ledgerTotals
	totalMu sync.Mutex
	samples []string
	onDone  func(c *callRec)
}

// ledgerTotals are the counts over the calls that the ledger finished.
type ledgerTotals struct {
	Calls      int            `json:"calls"`
	Bursts     int            `json:"bursts"`
	Expected   int            `json:"expected_deliveries"`
	Delivered  int            `json:"delivered"`
	Missing    int            `json:"missing"`
	Duplicates int            `json:"duplicates"`
	Wrong      int            `json:"wrong_target"`
	Altered    int            `json:"altered"`
	Shed       int            `json:"shed"`
	ShedOK     int            `json:"shed_delivered"`
	ByLabel    map[string]int `json:"calls_by_label"`
	MissingBy  map[string]int `json:"missing_by_label"`
	DupBy      map[string]int `json:"duplicates_by_label"`
	WrongBy    map[string]int `json:"wrong_by_label"`
}

func newLedger() *ledger {
	l := &ledger{epoch: time.Now(), latAll: &histogram{}, lat: map[string]*histogram{}}
	for i := range l.shards {
		l.shards[i].calls = map[uint]*callRec{}
	}
	for _, k := range []string{"voice", "data", "csbk", "other"} {
		l.lat[k] = &histogram{}
	}
	l.totals.ByLabel, l.totals.MissingBy, l.totals.DupBy, l.totals.WrongBy = map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	return l
}

func (l *ledger) now() int64 { return int64(time.Since(l.epoch)) }

func burstHash(raw []byte) uint32 {
	h := fnv.New32a()
	_, _ = h.Write(raw)
	return h.Sum32()
}

// register adds a transmission before its first burst is sent. want are
// the IDs of the targets that must get every burst.
func (l *ledger) register(c *callRec, bursts []burst, want []uint) error {
	c.want = append([]uint(nil), want...)
	sort.Slice(c.want, func(i, j int) bool { return c.want[i] < c.want[j] })
	c.wantIdx = make(map[uint]int, len(c.want))
	for i, w := range c.want {
		c.wantIdx[w] = i
	}
	for i := range c.byseq {
		c.byseq[i] = -1
	}
	c.sentAt = make([]int64, len(bursts))
	c.hash = make([]uint32, len(bursts))
	c.got = make([][]uint8, len(bursts))
	cells := make([]uint8, len(bursts)*len(c.want))
	for k, b := range bursts {
		c.got[k] = cells[k*len(c.want) : (k+1)*len(c.want)]
		c.hash[k] = burstHash(b.raw[:])
		if b.voiceFrame() {
			c.voiceN++
		} else {
			if c.byseq[k&0xFF] >= 0 {
				return fmt.Errorf("stream %d: bursts %d and %d have the same sequence number", c.stream, c.byseq[k&0xFF], k)
			}
			c.byseq[k&0xFF] = int32(k) //nolint:gosec
		}
	}
	c.extra = map[uint]int{}
	c.byLoad = make(map[uint32]int, len(bursts))
	c.lcBurst = make([]bool, len(bursts))
	for k, b := range bursts {
		if b.voiceFrame() {
			continue
		}
		c.lcBurst[k] = b.value == uint(dmrconst.DTypeVoiceHead) || b.value == uint(dmrconst.DTypeVoiceTerm)
		if _, dup := c.byLoad[payloadHash(b.raw)]; !dup {
			c.byLoad[payloadHash(b.raw)] = k
		}
	}
	l.bySD.Store(srcDst{c.src, c.dst, c.group}, c)
	sh := &l.shards[c.stream%64]
	sh.mu.Lock()
	sh.calls[c.stream] = c
	sh.mu.Unlock()
	return nil
}

func (l *ledger) get(stream uint) *callRec {
	sh := &l.shards[stream%64]
	sh.mu.RLock()
	defer sh.mu.RUnlock()
	return sh.calls[stream]
}

// willSend stamps burst k just before the generator writes it.
func (l *ledger) willSend(c *callRec, k int) {
	t := l.now()
	c.mu.Lock()
	c.sentAt[k] = t
	c.mu.Unlock()
	c.sent.Add(1)
	c.lastSent.Store(t)
	l.sent.Add(1)
}

// receive counts packet p at target.
func (l *ledger) receive(target uint, p *models.Packet) {
	now := l.now()
	l.recv.Add(1)
	if p.Src == parrotID {
		if v, ok := l.parrot.Load(target); ok {
			v.(*atomic.Int64).Add(1) //nolint:forcetypeassert
		} else {
			n := &atomic.Int64{}
			n.Add(1)
			if v, loaded := l.parrot.LoadOrStore(target, n); loaded {
				v.(*atomic.Int64).Add(1) //nolint:forcetypeassert
			}
		}
		return
	}
	c := l.get(p.StreamID)
	if c == nil {
		if v, ok := l.bySD.Load(srcDst{p.Src, p.Dst, p.GroupCall}); ok && v.(*callRec).foreign { //nolint:forcetypeassert
			l.deliver(target, v.(*callRec), layer2.RawBurst(p.DMRData), now, false) //nolint:forcetypeassert
			return
		}
		if l.stray.Add(1) <= 20 {
			l.sample(fmt.Sprintf("stray packet at %d: stream %d src %d dst %d frame %d/%d", target, p.StreamID, p.Src, p.Dst, p.FrameType, p.DTypeOrVSeq))
		}
		return
	}
	k, tagged := burstIndex(p)
	if !tagged {
		k = int(c.byseq[k])
	}
	if k < 0 || k >= len(c.sentAt) {
		l.badKey.Add(1)
		return
	}
	c.mu.Lock()
	altered := burstHash(p.DMRData[:]) != c.hash[k] || p.Src != c.src || p.Dst != c.dst || p.GroupCall != c.group
	if altered {
		c.altered++
	}
	c.mu.Unlock()
	l.count(target, c, k, now)
}

// deliver counts raw at target for a call that a transport carried in its
// own form: IPSC rebuilds the bursts, so a burst is known by its voice tag
// or its payload, and LC headers and terminators are rebuilt.
func (l *ledger) deliver(target uint, c *callRec, raw layer2.RawBurst, now int64, ipsc bool) {
	k := -1
	if v, ok := voiceTag(raw); ok {
		k = v
	} else if v, ok := c.byLoad[payloadHash(raw)]; ok {
		k = v
	}
	if k < 0 || k >= len(c.sentAt) {
		if ipsc || c.foreign {
			l.loose.Add(1)
			return
		}
		l.badKey.Add(1)
		return
	}
	l.count(target, c, k, now)
}

type ipscCall struct {
	target uint
	cc     uint32
}

// receiveIPSC counts an IPSC user packet at target.
func (l *ledger) receiveIPSC(target uint, b *testutils.IPSCBurst) {
	now := l.now()
	l.recv.Add(1)
	if b.Src == parrotID {
		return
	}
	u, err := ipscwire.DecodeUser(b.Data)
	if err != nil {
		l.badKey.Add(1)
		return
	}
	key := ipscCall{target, u.CallControl}
	v, ok := l.byCall.Load(key)
	if !ok {
		v, ok = l.bySD.Load(srcDst{b.Src, b.Dst, b.GroupCall})
		if !ok {
			if l.stray.Add(1) <= 20 {
				l.sample(fmt.Sprintf("stray IPSC packet at %d: src %d dst %d", target, b.Src, b.Dst))
			}
			return
		}
		v, _ = l.byCall.LoadOrStore(key, v)
	}
	raw, err := u.RawBurst(0, layer2.SyncSourceBS)
	if err != nil {
		l.loose.Add(1)
		return
	}
	l.deliver(target, v.(*callRec), raw, now, true) //nolint:forcetypeassert
}

func (l *ledger) count(target uint, c *callRec, k int, now int64) {
	c.mu.Lock()
	at := c.sentAt[k]
	if i, ok := c.wantIdx[target]; ok {
		if c.got[k][i] < 255 {
			c.got[k][i]++
		}
	} else {
		c.extra[target]++
	}
	kind := c.kind
	c.mu.Unlock()
	if at > 0 {
		d := time.Duration(now - at)
		l.latAll.add(d)
		h, ok := l.lat[kind]
		if !ok {
			h = l.lat["other"]
		}
		h.add(d)
	}
}

func (l *ledger) sample(s string) {
	l.totalMu.Lock()
	defer l.totalMu.Unlock()
	if len(l.samples) < 50 {
		l.samples = append(l.samples, s)
	}
}

// finish evaluates the calls whose last burst went out before grace ago,
// adds them to the totals and drops them. It returns the finished calls.
func (l *ledger) finish(grace time.Duration, all bool) {
	cut := l.now() - int64(grace)
	var out []*callRec
	for i := range l.shards {
		sh := &l.shards[i]
		sh.mu.Lock()
		for id, c := range sh.calls {
			if !c.done.Load() || (!all && c.lastSent.Load() > cut) {
				continue
			}
			delete(sh.calls, id)
			l.bySD.CompareAndDelete(srcDst{c.src, c.dst, c.group}, c)
			l.byCall.Range(func(k, v any) bool {
				if v == c {
					l.byCall.Delete(k)
				}
				return true
			})
			out = append(out, c)
		}
		sh.mu.Unlock()
	}
	for _, c := range out {
		l.tally(c)
		if l.onDone != nil {
			l.onDone(c)
		}
	}
}

func (l *ledger) tally(c *callRec) {
	c.mu.Lock()
	defer c.mu.Unlock()
	missing, dup, deliv := 0, 0, 0
	for k := range c.got {
		if c.sentAt[k] == 0 {
			continue
		}
		for i, n := range c.got[k] {
			if c.lcBurst[k] && (c.foreign || c.loose[c.want[i]]) {
				continue
			}
			if c.copies != nil {
				want := uint8(c.copies[k].Load()) //nolint:gosec
				switch {
				case n < want:
					missing += int(want - n)
					deliv += int(n)
				case n > want:
					dup += int(n - want)
					deliv += int(want)
				default:
					deliv += int(n)
				}
				continue
			}
			switch {
			case n == 0 && c.optional[c.want[i]]:
			case n == 0:
				missing++
			case n > 1:
				dup += int(n) - 1
				deliv++
			default:
				deliv++
			}
		}
	}
	wrong := 0
	for _, n := range c.extra {
		wrong += n
	}
	l.totalMu.Lock()
	defer l.totalMu.Unlock()
	t := &l.totals
	t.Calls++
	t.Bursts += int(c.sent.Load())
	if c.copies != nil {
		for k := range c.copies {
			t.Expected += int(c.copies[k].Load()) * len(c.want)
		}
	} else {
		t.Expected += int(c.sent.Load()) * len(c.want)
	}
	if c.shed {
		t.Shed += missing
		t.ShedOK += deliv
		missing = 0
	}
	t.Delivered += deliv
	t.Missing += missing
	t.Duplicates += dup
	t.Wrong += wrong
	t.Altered += c.altered
	t.ByLabel[c.label]++
	if missing > 0 {
		t.MissingBy[c.label] += missing
		if len(l.samples) < 50 {
			l.samples = append(l.samples, fmt.Sprintf("%s stream %d from %d to %d slot2=%v: %d of %d deliveries missing (targets %v)", c.label, c.stream, c.ingress, c.dst, c.slot2, missing, int(c.sent.Load())*len(c.want), c.missingTargets()))
		}
	}
	if dup > 0 {
		t.DupBy[c.label] += dup
		if len(l.samples) < 50 {
			l.samples = append(l.samples, fmt.Sprintf("%s stream %d from %d: %d duplicate deliveries", c.label, c.stream, c.ingress, dup))
		}
	}
	if wrong > 0 {
		t.WrongBy[c.label] += wrong
		if len(l.samples) < 50 {
			l.samples = append(l.samples, fmt.Sprintf("%s stream %d from %d to %d: delivered to targets that must not get it: %v", c.label, c.stream, c.ingress, c.dst, c.extra))
		}
	}
}

type srcDst struct {
	src, dst uint
	group    bool
}

// payloadHash is the hash of the info of a data burst, which every
// transport keeps.
func payloadHash(raw layer2.RawBurst) uint32 {
	dt := raw.SlotType().DataType
	var b []byte
	switch {
	case dt == enums.DataTypeRate34:
		p, _ := raw.Rate34Payload()
		b = p[:]
	case dt == enums.DataTypeRate1:
		p, _ := raw.Rate1Payload()
		b = p[:]
	case dt.IsBPTC() && dt != enums.DataTypeReserved:
		p, _ := raw.BPTCPayload()
		b = p[:]
	default:
		p := raw.Info()
		b = p[:]
	}
	return burstHash(append([]byte{byte(dt)}, b...))
}

func (c *callRec) voiceFrames() int { return c.voiceN }

func (c *callRec) missingTargets() map[uint]int {
	out := map[uint]int{}
	for k := range c.got {
		if c.sentAt[k] == 0 {
			continue
		}
		for i, n := range c.got[k] {
			if n == 0 && !c.optional[c.want[i]] {
				out[c.want[i]]++
			}
		}
	}
	return out
}

// reset forgets the totals, the samples and the latencies.
func (l *ledger) reset() {
	l.totalMu.Lock()
	l.totals = ledgerTotals{ByLabel: map[string]int{}, MissingBy: map[string]int{}, DupBy: map[string]int{}, WrongBy: map[string]int{}}
	l.samples = nil
	l.totalMu.Unlock()
	l.latAll.reset()
	for _, h := range l.lat {
		h.reset()
	}
	l.stray.Store(0)
	l.badKey.Store(0)
}

func (l *ledger) snapshot() (ledgerTotals, []string) {
	l.totalMu.Lock()
	defer l.totalMu.Unlock()
	return l.totals, append([]string(nil), l.samples...)
}

func (l *ledger) parrotCount(target uint) int64 {
	if v, ok := l.parrot.Load(target); ok {
		return v.(*atomic.Int64).Load() //nolint:forcetypeassert
	}
	return 0
}
