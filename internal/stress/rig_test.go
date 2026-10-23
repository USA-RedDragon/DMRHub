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
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

const (
	rptBase  = 310000
	userBase = 3100000
	tgBase   = 50000
	obPeerID = 3109001
	parrotID = 9990
	burstGap = 60 * time.Millisecond

	spRptID     = 319001
	ipscRptID   = 329001
	ipscSrcID   = 329002
	obSrcPeerID = 3109002
	xportUser   = 3109101
)

// topology is the network of the rig: repeater i is owned by user i and
// has talkgroup ts1[i] static on TS1 and ts2[i] on TS2.
type topology struct {
	n, groups int
	ts1, ts2  []int
}

func newTopology(n, fanout int) topology {
	g := max(2, (2*n+fanout-1)/max(fanout, 1))
	t := topology{n: n, groups: g, ts1: make([]int, n), ts2: make([]int, n)}
	for i := range n {
		t.ts1[i] = i % g
		t.ts2[i] = (i + 1) % g
	}
	return t
}

func rptID(i int) uint  { return uint(rptBase + i) }  //nolint:gosec
func userID(i int) uint { return uint(userBase + i) } //nolint:gosec
func tgID(g int) uint   { return uint(tgBase + g) }   //nolint:gosec

// groupTargets are the repeaters other than src that carry talkgroup g.
func (t topology) groupTargets(src, g int) []uint {
	var out []uint
	for i := range t.n {
		if i != src && (t.ts1[i] == g || t.ts2[i] == g) {
			out = append(out, rptID(i))
		}
	}
	return out
}

// rig is a running hub with its repeaters connected, a ledger and a
// sampler.
type rig struct {
	t *testing.T
	s settings
	// optional holds the repeaters that a step takes down on purpose, so
	// that what they miss is not counted as missing. Set it before the
	// lanes run.
	optional map[uint]bool
	res      *result
	logs     *logCounter
	stack    *testutils.IntegrationStack
	ctr      *testutils.Containers
	top      topology
	clients  []*testutils.MMDVMClient
	ob       *testutils.OpenBridgeClient
	led      *ledger
	samp     *sampler
	queries  *queryCounter
	streams  atomic.Uint32
	stop     chan struct{}
	wg       sync.WaitGroup
	recvWG   sync.WaitGroup
	lanes    atomic.Int32
	laneMu   []sync.Mutex
	lossyMu  sync.Mutex
	lossy    map[uint]bool
	extra    map[int][]uint
}

// lane is the lock of a slot of a repeater: a repeater carries one
// transmission per slot at a time, as on the air.
func (r *rig) lane(ingress uint, slot2 bool) *sync.Mutex {
	i := int(ingress) - rptBase
	if i < 0 || 2*i+1 >= len(r.laneMu) {
		return nil
	}
	if slot2 {
		return &r.laneMu[2*i+1]
	}
	return &r.laneMu[2*i]
}

type rigOptions struct {
	repeaters int
	withOB    bool
	noSample  bool
}

func newRig(t *testing.T, s settings, res *result, logs *logCounter, o rigOptions) *rig {
	t.Helper()
	if o.repeaters == 0 {
		o.repeaters = s.Repeaters
	}
	stack, ctr := newStack(t, s)
	r := &rig{t: t, s: s, res: res, logs: logs, stack: stack, ctr: ctr, top: newTopology(o.repeaters, s.Fanout), led: newLedger(), stop: make(chan struct{})}
	r.streams.Store(0x10000)
	r.laneMu = make([]sync.Mutex, 2*r.top.n)
	r.seed(t)
	r.queries = countQueries(stack.DB)
	stack.StartServers(t)
	if o.withOB {
		r.ob = testutils.NewOpenBridgeClient(obPeerID, obPassword)
		require.NoError(t, r.ob.Connect(stack.OpenBridgeAddr))
		addr := r.ob.LocalAddr()
		stack.SeedPeer(t, obPeerID, userID(0), obPassword, addr.IP.String(), addr.Port, false, true)
		stack.SeedPeerRule(t, obPeerID, false, 1, dmrconst.MaxDMRAddress)
		t.Cleanup(r.ob.Close)
	}
	r.connect(t)
	t.Cleanup(r.shutdown)
	if !o.noSample {
		r.samp = newSampler(stack, filepath.Join(s.Results, res.Area+".samples.csv"), logs, func() map[string]float64 {
			return map[string]float64{"sent": float64(r.led.sent.Load()), "received": float64(r.led.recv.Load()), "db_queries": float64(r.queries.total.Load())}
		})
		r.samp.run(time.Second)
	}
	return r
}

func (r *rig) seed(t *testing.T) {
	t.Helper()
	db := r.stack.DB
	users := make([]models.User, r.top.n)
	for i := range users {
		users[i] = models.User{ID: userID(i), Callsign: fmt.Sprintf("S%dX", i), Username: fmt.Sprintf("stress%d", i), Approved: true}
	}
	require.NoError(t, db.CreateInBatches(users, 200).Error)
	tgs := make([]models.Talkgroup, r.top.groups)
	for g := range tgs {
		tgs[g] = models.Talkgroup{ID: tgID(g), Name: fmt.Sprintf("TG%d", tgID(g))}
	}
	require.NoError(t, db.CreateInBatches(tgs, 200).Error)
	for i := range r.top.n {
		rp := models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{Callsign: fmt.Sprintf("S%dX", i), ID: rptID(i)},
			OwnerID:               userID(i),
			Type:                  models.RepeaterTypeMMDVM,
			Password:              rptPassword,
			TS1StaticTalkgroups:   []models.Talkgroup{{ID: tgID(r.top.ts1[i])}},
			TS2StaticTalkgroups:   []models.Talkgroup{{ID: tgID(r.top.ts2[i])}},
		}
		require.NoError(t, db.Create(&rp).Error)
	}
}

func (r *rig) connect(t *testing.T) {
	t.Helper()
	r.clients = make([]*testutils.MMDVMClient, r.top.n)
	errs := make(chan error, r.top.n)
	sem := make(chan struct{}, 16)
	var wg sync.WaitGroup
	for i := range r.top.n {
		c := testutils.NewMMDVMClient(uint32(rptID(i)), "", rptPassword) //nolint:gosec
		r.clients[i] = c
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			if err := c.Connect(r.stack.MMDVMAddr); err != nil {
				errs <- err
				return
			}
			if err := c.WaitReady(handshake); err != nil {
				errs <- fmt.Errorf("repeater %d: %w", rptID(i), err)
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	for i, c := range r.clients {
		r.listen(rptID(i), c.ReceivedPackets())
	}
	if r.ob != nil {
		r.listen(obPeerID, r.ob.ReceivedPackets())
	}
	time.Sleep(time.Second)
	r.heard(t)
}

// heard keys up each user once on the repeater it owns, so the hub knows
// where every radio is, and waits for the hub to settle.
func (r *rig) heard(t *testing.T) {
	t.Helper()
	var wg sync.WaitGroup
	for i := range r.top.n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			r.play(i, false, voiceCall("heard", true, tgID(r.top.ts1[i]), userID(i), 5, r.top.groupTargets(i, r.top.ts1[i])))
		}()
	}
	wg.Wait()
	time.Sleep(3 * time.Second)
	r.waitQuiet(time.Minute)
	r.led.finish(0, true)
	r.led.reset()
}

func (r *rig) listen(target uint, ch <-chan models.Packet) {
	r.recvWG.Add(1)
	go func() {
		defer r.recvWG.Done()
		for {
			select {
			case <-r.stop:
				return
			case p := <-ch:
				r.led.receive(target, &p)
			}
		}
	}()
}

func (r *rig) shutdown() {
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
	r.wg.Wait()
	r.drain()
	r.recvWG.Wait()
	for _, c := range r.clients {
		if c != nil {
			c.Close()
		}
	}
	if r.samp != nil {
		r.samp.finish()
	}
}

// drain waits for the hub to empty its queues after the traffic stops. A
// hub that cannot is stopped first, because stopping its servers while
// their channels from the hub are full hangs (see
// TestReproUnregisterServerWithBackedUpEgress).
func (r *rig) drain() {
	limit := envDuration("DMRHUB_STRESS_DRAIN", time.Minute)
	start := time.Now()
	for time.Since(start) < limit {
		if serverQueues(r.stack)+max(trackerQueue(r.stack), 0) == 0 {
			r.res.set("drain_s", time.Since(start).Seconds())
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.res.set("drain_s", -1)
	r.res.fail(r.t, "the hub still had %d packets queued to its servers and %d in the call tracker %s after the traffic stopped; stopping the hub before its servers",
		serverQueues(r.stack), trackerQueue(r.stack), limit)
	r.stack.Hub.Stop(context.Background())
}

// waitQuiet waits until the hub has no packet queued and has written no
// transmission row for two seconds.
func (r *rig) waitQuiet(limit time.Duration) {
	start := time.Now()
	var last int64 = -1
	quietSince := time.Now()
	for time.Since(start) < limit {
		var n int64
		quiet(r.stack.DB).Table("calls").Count(&n)
		busy := serverQueues(r.stack)+max(trackerQueue(r.stack), 0) > 0 || n != last
		if busy {
			last, quietSince = n, time.Now()
		} else if time.Since(quietSince) >= 2*time.Second {
			r.res.set("settle_s", time.Since(start).Seconds())
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
	r.res.set("settle_s", -1)
}

// script is a transmission that a lane plays.
type script struct {
	label   string
	kind    string
	group   bool
	dst     uint
	src     uint
	want    []uint
	bursts  []burst
	rowKind string
	sub     string
	text    string
	confirm bool
	noPace  bool
	// shed marks a flood that the hub may drop: its missing deliveries
	// count as shed, not missing.
	shed bool
	err  error
}

func voiceCall(label string, group bool, dst, src uint, frames int, want []uint) script {
	b, err := voiceScript{group: group, dst: dst, src: src, frames: frames}.bursts()
	return script{label: label, kind: "voice", group: group, dst: dst, src: src, want: want, bursts: b, rowKind: "voice", err: err}
}

// play sends the script s from repeater i on slot2 at the pace of the air
// interface and returns the record of the transmission.
func (r *rig) play(i int, slot2 bool, s script) *callRec {
	return r.playOn(r.clients[i].SendDMRD, rptID(i), slot2, s, nil)
}

func (r *rig) playOn(send func(models.Packet) error, ingress uint, slot2 bool, s script, stop <-chan struct{}) *callRec {
	if s.err != nil {
		r.res.fail(r.t, "building %s: %v", s.label, s.err)
		return nil
	}
	if mu := r.lane(ingress, slot2); mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	stream := uint(r.streams.Add(1))
	c := &callRec{stream: stream, kind: s.kind, label: s.label, src: s.src, dst: s.dst, group: s.group, slot2: slot2, ingress: ingress, rowKind: s.rowKind, sub: s.sub, text: s.text, confirm: s.confirm}
	if s.kind == "voice" && s.dst == parrotID {
		c.kind = "parrot"
	}
	c.loose = map[uint]bool{ipscRptID: true}
	c.shed = s.shed
	c.optional = r.optional
	r.lossyMu.Lock()
	if r.lossy[ingress] {
		c.copies = make([]atomic.Int32, len(s.bursts))
	}
	r.lossyMu.Unlock()
	if err := r.led.register(c, s.bursts, s.want); err != nil {
		r.res.fail(r.t, "%v", err)
		return nil
	}
	next := time.Now()
	for k, b := range s.bursts {
		if d := time.Until(next); d > 0 && !s.noPace {
			select {
			case <-time.After(d):
			case <-stop:
				c.done.Store(true)
				return c
			}
		}
		next = next.Add(burstGap)
		p := packetOf(b, k, s.src, s.dst, stream, s.group, slot2)
		r.led.willSend(c, k)
		if err := send(p); err != nil {
			r.res.inc("send_errors")
		}
	}
	c.done.Store(true)
	return c
}

// mix is a weighted set of script makers for a lane.
type mix []struct {
	weight int
	make   func(r *rig, rnd *rand.Rand, i int, slot2 bool) script
}

func (m mix) pick(rnd *rand.Rand) int {
	total := 0
	for _, e := range m {
		total += e.weight
	}
	n := rnd.IntN(total)
	for i, e := range m {
		if n < e.weight {
			return i
		}
		n -= e.weight
	}
	return len(m) - 1
}

// runLanes plays the mix on both slots of every repeater until d passes.
// A lane rests between transmissions so that it is busy for duty of the
// time, and sometimes keys up again at once.
func (r *rig) runLanes(ctx context.Context, d time.Duration, m mix, duty float64, repeaters int, skip func(i int, slot2 bool) bool) {
	deadline := time.Now().Add(d)
	if repeaters <= 0 || repeaters > r.top.n {
		repeaters = r.top.n
	}
	for i := range repeaters {
		for _, slot2 := range []bool{false, true} {
			if skip != nil && skip(i, slot2) {
				continue
			}
			r.wg.Add(1)
			go func() {
				defer r.wg.Done()
				r.lanes.Add(1)
				defer r.lanes.Add(-1)
				rnd := rand.New(rand.NewPCG(uint64(i), map[bool]uint64{false: 1, true: 2}[slot2])) //nolint:gosec
				time.Sleep(time.Duration(rnd.Int64N(int64(2 * time.Second))))
				for time.Now().Before(deadline) && ctx.Err() == nil {
					s := m[m.pick(rnd)].make(r, rnd, i, slot2)
					start := time.Now()
					r.playOn(r.clients[i].SendDMRD, rptID(i), slot2, s, r.stop)
					busy := time.Since(start)
					if rnd.IntN(10) == 0 {
						continue
					}
					rest := time.Duration(float64(busy) * (1 - duty) / duty)
					rest += time.Duration(rnd.Int64N(int64(500 * time.Millisecond)))
					select {
					case <-time.After(rest):
					case <-r.stop:
						return
					case <-ctx.Done():
						return
					}
				}
			}()
		}
	}
}

// standard script makers of the call mix.
func groupVoice(label string, frames func(*rand.Rand) int) func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
	return func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
		g := r.top.ts1[i]
		if slot2 {
			g = r.top.ts2[i]
		}
		s := voiceCall(label, true, tgID(g), userID(i), frames(rnd), r.groupWant(i, g))
		return s
	}
}

func (r *rig) groupWant(i, g int) []uint {
	w := append(r.top.groupTargets(i, g), r.extra[g]...)
	if r.ob != nil {
		w = append(w, obPeerID)
	}
	return w
}

func (r *rig) privateWant(target uint) []uint {
	w := []uint{target}
	if r.ob != nil {
		w = append(w, obPeerID)
	}
	return w
}

func otherRepeater(rnd *rand.Rand, n, i int) int {
	j := rnd.IntN(n - 1)
	if j >= i {
		j++
	}
	return j
}

func privateToUser(frames func(*rand.Rand) int) func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
	return func(r *rig, rnd *rand.Rand, i int, _ bool) script {
		j := otherRepeater(rnd, r.top.n, i)
		return voiceCall("private-user", false, userID(j), userID(i), frames(rnd), r.privateWant(rptID(j)))
	}
}

func privateToRepeater(frames func(*rand.Rand) int) func(r *rig, rnd *rand.Rand, i int, slot2 bool) script {
	return func(r *rig, rnd *rand.Rand, i int, _ bool) script {
		j := otherRepeater(rnd, r.top.n, i)
		return voiceCall("private-repeater", false, rptID(j), userID(i), frames(rnd), r.privateWant(rptID(j)))
	}
}

func between(lo, hi int) func(*rand.Rand) int {
	return func(rnd *rand.Rand) int { return lo + rnd.IntN(hi-lo+1) }
}
