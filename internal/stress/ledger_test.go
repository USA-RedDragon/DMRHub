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
	"math"
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
	dropped  map[uint]map[int]bool
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
	latCalm *histogram
	stray   atomic.Int64
	badKey  atomic.Int64
	recv    atomic.Int64
	sent    atomic.Int64
	reports atomic.Int64
	unknown atomic.Int64
	loose   atomic.Int64
	bySD    sync.Map
	byCall  sync.Map
	parrot  sync.Map
	totals  ledgerTotals
	totalMu sync.Mutex
	samples []string
	onDone  func(c *callRec)
	windows atomic.Pointer[[]*sentWindow]
}

// sentWindow is the latency of the bursts sent between from and to, in
// ledger time.
type sentWindow struct {
	from int64
	to   atomic.Int64
	lat  histogram
}

// window starts a sentWindow at now, which end closes.
func (l *ledger) window() (w *sentWindow, end func()) {
	w = &sentWindow{from: l.now()}
	w.to.Store(math.MaxInt64)
	for {
		old := l.windows.Load()
		var next []*sentWindow
		if old != nil {
			next = append(next, *old...)
		}
		next = append(next, w)
		if l.windows.CompareAndSwap(old, &next) {
			return w, func() { w.to.Store(l.now()) }
		}
	}
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
	SlotBusy   int            `json:"slot_busy"`
	SlotBusyOK int            `json:"slot_busy_delivered"`
	ByLabel    map[string]int `json:"calls_by_label"`
	MissingBy  map[string]int `json:"missing_by_label"`
	MissingAt  map[uint]int   `json:"missing_by_target"`
	DupBy      map[string]int `json:"duplicates_by_label"`
	WrongBy    map[string]int `json:"wrong_by_label"`
}

func newLedger() *ledger {
	l := &ledger{epoch: time.Now(), latAll: &histogram{}, latCalm: &histogram{}, lat: map[string]*histogram{}}
	for i := range l.shards {
		l.shards[i].calls = map[uint]*callRec{}
	}
	for _, k := range []string{"voice", "data", "csbk", "other", "ipsc_slot"} {
		l.lat[k] = &histogram{}
	}
	l.totals.ByLabel, l.totals.MissingBy, l.totals.DupBy, l.totals.WrongBy = map[string]int{}, map[string]int{}, map[string]int{}, map[string]int{}
	l.totals.MissingAt = map[uint]int{}
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
	l.countAt(target, c, k, now, ipsc)
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
	raw, rawErr := u.RawBurst(0, layer2.SyncSourceBS)
	if rawErr == nil {
		if stream, ok := voiceTagStream(raw); ok {
			if c := l.get(stream); c != nil {
				l.deliver(target, c, raw, now, true)
				return
			}
		}
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
	if rawErr != nil {
		l.loose.Add(1)
		return
	}
	l.deliver(target, v.(*callRec), raw, now, true) //nolint:forcetypeassert
}

// slotBusy notes that the IPSC server dropped burst p to the target
// repeater p.Repeater and reported it slot_busy: the slot of the repeater
// carries one call at a time, and the calls that waited for it too long or
// found too many calls waiting are dropped. Such a burst is not missing.
func (l *ledger) slotBusy(p *models.Packet) {
	l.reports.Add(1)
	c := l.get(p.StreamID)
	if c == nil {
		v, ok := l.bySD.Load(srcDst{p.Src, p.Dst, p.GroupCall})
		if !ok || !v.(*callRec).foreign { //nolint:forcetypeassert
			l.unknown.Add(1)
			return
		}
		c = v.(*callRec) //nolint:forcetypeassert
	}
	raw := layer2.RawBurst(p.DMRData)
	k := -1
	if v, ok := voiceTag(raw); ok {
		k = v
	} else if v, ok := c.byLoad[payloadHash(raw)]; ok && c.foreign {
		k = v
	} else if !c.foreign {
		k = int(c.byseq[p.Seq&0xFF])
	}
	if k < 0 || k >= len(c.sentAt) {
		l.unknown.Add(1)
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.dropped == nil {
		c.dropped = map[uint]map[int]bool{}
	}
	if c.dropped[p.Repeater] == nil {
		c.dropped[p.Repeater] = map[int]bool{}
	}
	c.dropped[p.Repeater][k] = true
}

func (l *ledger) count(target uint, c *callRec, k int, now int64) {
	l.countAt(target, c, k, now, false)
}

func (l *ledger) countAt(target uint, c *callRec, k int, now int64, ipsc bool) {
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
		if ipsc {
			l.lat["ipsc_slot"].add(d)
			return
		}
		l.latAll.add(d)
		calm := true
		if ws := l.windows.Load(); ws != nil {
			for _, w := range *ws {
				if at >= w.from && at <= w.to.Load() {
					w.lat.add(d)
					calm = false
				}
			}
		}
		if calm {
			l.latCalm.add(d)
		}
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
	missing, dup, deliv, dropped, droppedOK := 0, 0, 0, 0, 0
	missingAt := map[uint]int{}
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
			reported := c.dropped[c.want[i]][k]
			switch {
			case reported && n > 0:
				droppedOK++
			case reported:
				dropped++
				continue
			}
			switch {
			case n == 0 && c.optional[c.want[i]]:
			case n == 0:
				missing++
				missingAt[c.want[i]]++
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
	t.SlotBusy += dropped
	t.SlotBusyOK += droppedOK
	t.Missing += missing
	t.Duplicates += dup
	t.Wrong += wrong
	t.Altered += c.altered
	t.ByLabel[c.label]++
	if missing > 0 {
		t.MissingBy[c.label] += missing
		for target, n := range missingAt {
			t.MissingAt[target] += n
		}
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
			if n == 0 && !c.optional[c.want[i]] && !c.dropped[c.want[i]][k] {
				out[c.want[i]]++
			}
		}
	}
	return out
}

// reset forgets the totals, the samples and the latencies.
func (l *ledger) reset() {
	l.totalMu.Lock()
	l.totals = ledgerTotals{ByLabel: map[string]int{}, MissingBy: map[string]int{}, MissingAt: map[uint]int{}, DupBy: map[string]int{}, WrongBy: map[string]int{}}
	l.samples = nil
	l.totalMu.Unlock()
	l.latAll.reset()
	l.latCalm.reset()
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
