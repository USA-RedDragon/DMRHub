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
	"context"
	"fmt"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

var smsCounter atomic.Int64 //nolint:gochecknoglobals

func smsScript(label string, group bool, dst, src uint, want []uint, f sms.Format, rate enums.DataType) script {
	text := fmt.Sprintf("STRESS %d", smsCounter.Add(1))
	b, _, err := smsBursts(f, group, dst, src, text, rate, false)
	return script{label: label, kind: "data", group: group, dst: dst, src: src, want: want, bursts: b, rowKind: "data", text: text, err: err}
}

// callMix is the mix of the call mix area: group and private calls on both
// slots, kerchunks, late entry, lost terminators, the parrot and SMS.
func callMix() mix {
	return mix{
		{30, groupVoice("group", between(17, 83))},
		{10, groupVoice("kerchunk", between(5, 5))},
		{8, func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
			g := r.lanesTG(i, slot2)
			b, err := voiceScript{group: true, dst: tgID(g), src: userID(i), frames: between(18, 60)(rnd), late: true, lateFirst: rnd.IntN(6)}.bursts()
			return script{label: "late-entry", kind: "voice", group: true, dst: tgID(g), src: userID(i), want: r.groupWant(i, g), bursts: b, rowKind: "voice", err: err}
		}},
		{6, func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
			g := r.lanesTG(i, slot2)
			b, err := voiceScript{group: true, dst: tgID(g), src: userID(i), frames: between(18, 60)(rnd), noTerm: true}.bursts()
			return script{label: "lost-terminator", kind: "voice", group: true, dst: tgID(g), src: userID(i), want: r.groupWant(i, g), bursts: b, rowKind: "voice", err: err}
		}},
		{12, privateToUser(between(17, 60))},
		{6, privateToRepeater(between(17, 60))},
		{3, func(r *rig, rnd *rand.Rand, i int, _ bool) script {
			s := voiceCall("parrot", false, parrotID, userID(i), between(17, 50)(rnd), nil)
			return s
		}},
		{5, func(r *rig, _ *rand.Rand, i int, slot2 bool) script {
			g := r.lanesTG(i, slot2)
			return smsScript("sms-group", true, tgID(g), userID(i), r.groupWant(i, g), sms.FormatIP, enums.DataTypeRate12)
		}},
		{5, func(r *rig, rnd *rand.Rand, i int, _ bool) script {
			j := otherRepeater(rnd, r.top.n, i)
			return smsScript("sms-private", false, userID(j), userID(i), r.privateWant(rptID(j)), sms.FormatIP, enums.DataTypeRate12)
		}},
	}
}

func (r *rig) lanesTG(i int, slot2 bool) int {
	if slot2 {
		return r.top.ts2[i]
	}
	return r.top.ts1[i]
}

// collect keeps every call that the ledger finishes.
type collector struct {
	mu    sync.Mutex
	calls []*callRec
}

func (c *collector) add(rec *callRec) {
	light := &callRec{
		stream: rec.stream, kind: rec.kind, label: rec.label, src: rec.src, dst: rec.dst, group: rec.group, slot2: rec.slot2,
		ingress: rec.ingress, rowKind: rec.rowKind, text: rec.text, sub: rec.sub, confirm: rec.confirm, voiceN: rec.voiceN,
	}
	light.sent.Store(rec.sent.Load())
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, light)
}

func (c *collector) all() []*callRec {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]*callRec(nil), c.calls...)
}

// TestCallMix runs the call mix on every repeater at once and checks that
// each burst reached exactly its targets once, that each transmission has
// one correct row and that no stream is left open.
func TestCallMix(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "callmix")
	r := newRig(t, s, res, logs, rigOptions{withOB: true})
	col := &collector{}
	r.led.onDone = col.add

	long := s.Long
	if long == 0 {
		long = min(3*time.Minute, s.Duration*8/10)
	}
	res.set("long_call_s", long.Seconds())
	var longCall *callRec
	var longWG sync.WaitGroup
	longWG.Add(1)
	go func() {
		defer longWG.Done()
		frames := int(long / burstGap)
		longCall = r.play(0, false, voiceCall("long", true, tgID(r.top.ts1[0]), userID(0), frames, r.groupWant(0, r.top.ts1[0])))
	}()
	r.runLanes(context.Background(), s.Duration, callMix(), s.Duty, 0, func(i int, slot2 bool) bool { return i == 0 && !slot2 })
	r.waitLanes()
	longWG.Wait()
	time.Sleep(10 * time.Second)
	r.led.finish(0, true)

	tot := r.checkDelivery()
	res.set("parallel_lanes", float64(2*r.top.n))
	calls := col.all()
	r.checkRows(calls)
	r.checkMessages(calls)
	r.checkParrot(calls)
	r.checkNoStuckStreams(15 * time.Second)
	if longCall != nil {
		res.set("long_call_bursts", float64(longCall.sent.Load()))
	}
	t.Logf("call mix: %d calls, %d bursts, %d of %d deliveries, latency %+v", tot.Calls, tot.Bursts, tot.Delivered, tot.Expected, res.Latency["all"])
}

// checkMessages checks that each SMS has exactly one message row with its
// text, and that a confirmed one has the ACK as its response. A retry
// after a NACK sends the same text again and still makes one row.
func (r *rig) checkMessages(calls []*callRec) {
	var texts []string
	want := map[string]*callRec{}
	for _, c := range calls {
		if c.text != "" {
			if _, dup := want[c.text]; !dup {
				texts = append(texts, c.text)
			}
			want[c.text] = c
		}
	}
	type msgRow struct {
		StreamID  uint
		Text      string
		Confirmed bool
		Response  string
	}
	got := map[string][]msgRow{}
	for i := 0; i < len(texts); i += 500 {
		var rows []msgRow
		if err := quiet(r.stack.DB).Table("messages").Select("stream_id, text, confirmed, response").Where("text IN ?", texts[i:min(i+500, len(texts))]).Scan(&rows).Error; err != nil {
			r.res.fail(r.t, "reading the message rows: %v", err)
			return
		}
		for _, m := range rows {
			got[m.Text] = append(got[m.Text], m)
		}
	}
	bad := map[string]int{}
	var samples []string
	for text, c := range want {
		ms := got[text]
		if len(ms) != 1 || (c.confirm && (!ms[0].Confirmed || ms[0].Response != "ack")) {
			bad[c.label]++
			if len(samples) < 8 {
				samples = append(samples, fmt.Sprintf("%s stream %d %q: %s", c.label, c.stream, text, describeMessages(ms)))
			}
		}
	}
	n := 0
	for _, v := range bad {
		n += v
	}
	r.res.set("messages_checked", float64(len(want)))
	r.res.set("messages_bad", float64(n))
	if n > 0 {
		r.res.fail(r.t, "%d of %d SMS have no single correct message row (by kind %v): %v", n, len(want), bad, samples)
	}
}

func describeMessages[T any](ms []T) string {
	return fmt.Sprintf("%+v", ms)
}

// checkParrot checks that each repeater that called the parrot got its
// calls played back, and only it. The parrot builds its reply anew, with
// its own headers and whole superframes, so a reply has at least the voice
// bursts of the call and at most a superframe and a few headers more.
func (r *rig) checkParrot(calls []*callRec) {
	frames, extra := map[uint]int64{}, map[uint]int64{}
	for _, c := range calls {
		if c.kind != "parrot" {
			continue
		}
		frames[c.ingress] += int64(c.voiceFrames())
		extra[c.ingress] += 12
	}
	var total, got int64
	for i := range r.top.n {
		id := rptID(i)
		n := r.led.parrotCount(id)
		got += n
		total += frames[id]
		if frames[id] == 0 && n > 0 {
			r.res.fail(r.t, "repeater %d got %d parrot bursts but never called the parrot", id, n)
		}
		if frames[id] > 0 && (n < frames[id] || n > frames[id]+extra[id]) {
			r.res.fail(r.t, "repeater %d sent %d voice bursts to the parrot and got %d back", id, frames[id], n)
		}
	}
	if n := r.led.parrotCount(obPeerID); n > 0 {
		r.res.fail(r.t, "the OpenBridge peer got %d parrot bursts", n)
	}
	r.res.set("parrot_voice_bursts_sent", float64(total))
	r.res.set("parrot_bursts_played", float64(got))
}
