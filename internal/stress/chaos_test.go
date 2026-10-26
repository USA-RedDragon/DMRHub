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
	"errors"
	"math/rand/v2"
	"net"
	"os/exec"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/clockstep"
	"github.com/stretchr/testify/require"
)

// impairments of the path from a repeater to the hub, in percent of the
// DMRD packets.
type impairments struct {
	loss, dup, reorder int
}

// proxy relays the UDP packets of one repeater to the hub and back, and
// loses, duplicates and reorders the DMRD packets from the repeater. It
// tells the ledger how many copies of each burst it passed on.
type proxy struct {
	in       *net.UDPConn
	out      *net.UDPConn
	client   atomic.Pointer[net.UDPAddr]
	imp      impairments
	led      *ledger
	rnd      *rand.Rand
	held     []byte
	done     chan struct{}
	wg       sync.WaitGroup
	lost     atomic.Int64
	duped    atomic.Int64
	swapped  atomic.Int64
	passed   atomic.Int64
	stopOnce sync.Once
}

func newProxy(t *testing.T, hub string, imp impairments, led *ledger, seed uint64) *proxy {
	t.Helper()
	in, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	raddr, err := net.ResolveUDPAddr("udp", hub)
	require.NoError(t, err)
	out, err := net.DialUDP("udp", nil, raddr)
	require.NoError(t, err)
	p := &proxy{in: in, out: out, imp: imp, led: led, rnd: rand.New(rand.NewPCG(seed, seed)), done: make(chan struct{})} //nolint:gosec
	p.wg.Add(2)
	go p.up()
	go p.down()
	t.Cleanup(p.close)
	return p
}

func (p *proxy) addr() string { return p.in.LocalAddr().String() }

func (p *proxy) close() {
	p.stopOnce.Do(func() {
		close(p.done)
		_ = p.in.Close()
		_ = p.out.Close()
	})
	p.wg.Wait()
}

func (p *proxy) up() {
	defer p.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, from, err := p.in.ReadFromUDP(buf)
		if err != nil {
			return
		}
		p.client.Store(from)
		pkt := append([]byte(nil), buf[:n]...)
		if n < 4 || string(pkt[:4]) != "DMRD" {
			_, _ = p.out.Write(pkt)
			continue
		}
		r := p.rnd.IntN(100)
		switch {
		case r < p.imp.loss:
			p.lost.Add(1)
			continue
		case r < p.imp.loss+p.imp.dup:
			p.forward(pkt)
			p.duped.Add(1)
			p.forward(pkt)
		case r < p.imp.loss+p.imp.dup+p.imp.reorder && p.held == nil:
			p.held = pkt
			p.swapped.Add(1)
		default:
			p.forward(pkt)
			if p.held != nil {
				p.forward(p.held)
				p.held = nil
			}
		}
	}
}

func (p *proxy) forward(pkt []byte) {
	if d, ok := models.UnpackPacket(pkt); ok {
		if c := p.led.get(d.StreamID); c != nil && c.copies != nil {
			k, tagged := burstIndex(&d)
			if !tagged {
				k = int(c.byseq[k])
			}
			if k >= 0 && k < len(c.copies) {
				c.copies[k].Add(1)
			}
		}
	}
	p.passed.Add(1)
	_, _ = p.out.Write(pkt)
}

func (p *proxy) down() {
	defer p.wg.Done()
	buf := make([]byte, 2048)
	for {
		n, err := p.out.Read(buf)
		if err != nil {
			select {
			case <-p.done:
				return
			default:
			}
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if to := p.client.Load(); to != nil {
			_, _ = p.in.WriteToUDP(buf[:n], to)
		}
	}
}

// reconnect replaces the client of repeater i with a new one that logs in
// to addr, as a repeater does after a restart.
func (r *rig) reconnect(t *testing.T, i int, addr string) {
	t.Helper()
	if mu := r.lane(rptID(i), false); mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}
	r.clients[i].Close()
	c := testutils.NewMMDVMClient(uint32(rptID(i)), "", rptPassword) //nolint:gosec
	require.NoError(t, c.Connect(addr))
	require.NoError(t, c.WaitReady(handshake))
	r.clients[i] = c
	r.listen(rptID(i), c.ReceivedPackets())
}

// probe sends one short group call from repeater i every interval until
// the call reaches all its targets or timeout passes, and returns the time
// it took.
func (r *rig) probe(i int, timeout time.Duration) (time.Duration, bool) {
	start := time.Now()
	for time.Since(start) < timeout {
		c := r.play(i, true, voiceCall("probe", true, tgID(r.top.ts2[i]), userID(i), 3, r.top.groupTargets(i, r.top.ts2[i])))
		time.Sleep(500 * time.Millisecond)
		if c != nil && c.allDelivered() {
			return time.Since(start), true
		}
	}
	return time.Since(start), false
}

func (c *callRec) allDelivered() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for k := range c.got {
		for _, n := range c.got[k] {
			if n == 0 {
				return false
			}
		}
	}
	return true
}

func docker(args ...string) error {
	out, err := exec.Command("docker", args...).CombinedOutput() //nolint:gosec,noctx
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

// TestChaos runs the call mix on most repeaters and, meanwhile, breaks
// things one at a time: a repeater that vanishes mid-call and logs in
// again, a lossy, duplicating and reordering path, a restart of the MMDVM
// server, a Redis and a Postgres outage, the failover of a replica, and a
// step of the wall clock an hour forward and an hour back. The clock steps
// run only in the throwaway VM of hack/clockstep.sh, which
// DMRHUB_STRESS_CLOCKSTEP=1 hack/stress.sh starts. After each it checks
// that the hub recovers without a restart and that nothing stalls. The
// background load is DMRHUB_STRESS_CHAOS_BUSY busy repeaters (default 3),
// below the point where the load ramp degrades, so that what goes wrong is
// the fault's doing.
func TestChaos(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "chaos")
	r := newRig(t, s, res, logs, rigOptions{})
	n := r.top.n
	require.GreaterOrEqual(t, n, 6, "chaos needs at least 6 repeaters")
	lossy, dropper, prober := n-1, n-2, n-3
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	busy := min(n-3, envInt("DMRHUB_STRESS_CHAOS_BUSY", 3))
	res.set("busy_repeaters", float64(busy))
	r.runLanes(ctx, 10*time.Hour, callMix(), s.Duty*0.7, busy, nil)
	recovery := map[string]float64{}
	outages := map[string]*sentWindow{}

	step(t, "peer-drop", func(t *testing.T) {
		stop := make(chan struct{})
		go func() {
			time.Sleep(time.Second)
			r.clients[dropper].Close()
			close(stop)
		}()
		r.playOn(r.clients[dropper].SendDMRD, rptID(dropper), false, voiceCall("dropped", true, tgID(r.top.ts1[dropper]), userID(dropper), 100, r.groupWant(dropper, r.top.ts1[dropper])), stop)
		r.reconnect(t, dropper, r.stack.MMDVMAddr)
		d, ok := r.probe(dropper, 15*time.Second)
		recovery["peer-drop"] = d.Seconds()
		if !ok {
			res.fail(t, "a repeater that dropped mid-call and logged in again got no call through within 15s")
		}
	})

	step(t, "loss-dup-reorder", func(t *testing.T) {
		p := newProxy(t, r.stack.MMDVMAddr, impairments{loss: 5, dup: 5, reorder: 5}, r.led, 11)
		r.reconnect(t, lossy, p.addr())
		for range 10 {
			c := r.lossyCall(lossy)
			_ = c
		}
		res.set("proxy_lost", float64(p.lost.Load()))
		res.set("proxy_duplicated", float64(p.duped.Load()))
		res.set("proxy_reordered", float64(p.swapped.Load()))
		r.reconnect(t, lossy, r.stack.MMDVMAddr)
	})

	step(t, "mmdvm-restart-handoff", func(t *testing.T) {
		r.stack.RestartMMDVM(t, testutils.RestartHandoff)
		d, ok := r.probe(prober, 20*time.Second)
		recovery["mmdvm-restart-handoff"] = d.Seconds()
		if !ok {
			res.fail(t, "no call got through within 20s of a handoff restart of the MMDVM server")
		}
	})

	for _, svc := range []string{"redis", "postgres"} {
		step(t, svc+"-outage", func(t *testing.T) {
			id := r.ctr.Redis
			if svc == "postgres" {
				id = r.ctr.Postgres
			}
			if id == "" {
				t.Skip("needs DMRHUB_STRESS_BACKEND=postgres-redis")
			}
			outage := 5 * time.Second
			w, end := r.led.window()
			outages[svc] = w
			require.NoError(t, docker("pause", id))
			time.Sleep(outage)
			require.NoError(t, docker("unpause", id))
			end()
			d, ok := r.probe(prober, 30*time.Second)
			recovery[svc+"-outage"] = d.Seconds()
			if !ok {
				res.fail(t, "no call got through within 30s after a %s outage of %s", svc, outage)
			}
		})
	}

	step(t, "replica-failover", func(t *testing.T) {
		r.checkReplicaFailover(t, recovery)
	})

	res.set("clock_jump_tested", 0)
	for _, jump := range []struct {
		name string
		by   time.Duration
	}{{"clock-jump", time.Hour}, {"clock-jump-back", -time.Hour}} {
		step(t, jump.name, func(t *testing.T) {
			clockstep.Step(t, jump.by)
			res.set("clock_jump_tested", 1)
			d, ok := r.probe(prober, 15*time.Second)
			recovery[jump.name] = d.Seconds()
			if !ok {
				res.fail(t, "no call got through within 15s of a %s step of the wall clock", jump.by)
			}
		})
	}

	cancel()
	r.waitLanes()
	time.Sleep(8 * time.Second)
	r.led.finish(0, true)
	tot := r.checkDeliveryLenient()
	r.checkOutageLatency(outages)
	res.extra("recovery_s", recovery)
	r.checkNoStuckStreams(20 * time.Second)
	r.checkLateDeliveries()
	t.Logf("chaos: %+v recovery %v", tot, recovery)
}

// lossyCall plays a group call from repeater i whose path loses,
// duplicates and reorders packets; the ledger expects at each target as
// many copies of a burst as the proxy passed on.
func (r *rig) lossyCall(i int) *callRec {
	r.lossyMu.Lock()
	if r.lossy == nil {
		r.lossy = map[uint]bool{}
	}
	r.lossy[rptID(i)] = true
	r.lossyMu.Unlock()
	defer func() {
		r.lossyMu.Lock()
		delete(r.lossy, rptID(i))
		r.lossyMu.Unlock()
	}()
	g := r.top.ts1[i]
	return r.play(i, false, voiceCall("lossy", true, tgID(g), userID(i), 40, r.groupWant(i, g)))
}

// checkDeliveryLenient records the delivery totals of a chaos run. Bursts
// sent while something was down may be lost, but every loss must be
// visible: a burst is either delivered promptly or counted as lost.
func (r *rig) checkDeliveryLenient() ledgerTotals {
	tot, samples := r.led.snapshot()
	r.res.set("calls", float64(tot.Calls))
	r.res.set("deliveries_expected", float64(tot.Expected))
	r.res.set("deliveries_ok", float64(tot.Delivered))
	r.res.set("deliveries_missing", float64(tot.Missing))
	r.res.set("deliveries_duplicate", float64(tot.Duplicates))
	r.res.set("deliveries_wrong_target", float64(tot.Wrong))
	r.res.extra("totals", tot)
	r.res.extra("samples", samples)
	r.res.mu.Lock()
	r.res.Latency = map[string]latencySummary{"all": r.led.latAll.summary()}
	r.res.mu.Unlock()
	if tot.Wrong > 0 {
		r.res.fail(r.t, "%d deliveries went to targets that must not get them", tot.Wrong)
	}
	if lossy := tot.MissingBy["lossy"] + tot.DupBy["lossy"]; lossy > 0 {
		r.res.fail(r.t, "the bursts of the lossy path did not reach the targets as often as the proxy passed them: %d missing, %d extra", tot.MissingBy["lossy"], tot.DupBy["lossy"])
	}
	return tot
}

// checkOutageLatency records the latency of the bursts sent during each
// outage.
func (r *rig) checkOutageLatency(outages map[string]*sentWindow) {
	for svc, w := range outages {
		s := w.lat.summary()
		r.res.mu.Lock()
		r.res.Latency[svc+"-outage"] = s
		r.res.mu.Unlock()
		r.t.Logf("bursts sent during the %s outage: %d delivered, p50 %.1f ms, p99 %.1f ms, max %.1f ms", svc, s.Count, s.P50, s.P99, s.Max)
	}
}

// checkLateDeliveries fails when a burst sent outside an outage arrived
// more than 5 s after it was sent. The hub holds the bursts sent during an
// outage until the fault ends, and checkOutageLatency reports them; it
// must not hold the bursts sent after it.
func (r *rig) checkLateDeliveries() {
	r.res.set("latency_max_ms", r.led.latAll.summary().Max)
	s := r.led.latCalm.summary()
	r.res.set("latency_max_outside_outages_ms", s.Max)
	if s.Max > 5000 {
		r.res.fail(r.t, "a burst sent outside an outage arrived %.1f s after it was sent", s.Max/1000)
	}
}

// checkReplicaFailover starts a second replica, puts a session peer on it
// and kills the replica; the peer then logs in to the first, as a peer
// behind a load balancer does, and its calls must go through again.
func (r *rig) checkReplicaFailover(t *testing.T, recovery map[string]float64) {
	const id = 319700
	r.stack.SeedMMDVMRepeater(t, id, userID(0), rptPassword)
	r.stack.AssignTS1StaticTG(t, id, tgID(r.top.ts1[0]))
	rep := r.stack.SpawnSecondReplica(t)
	sp, err := testutils.NewSessionPeer(id, rptPassword, "")
	require.NoError(t, err)
	t.Cleanup(sp.Close)
	require.NoError(t, sp.Connect(rep.MMDVMAddr))
	require.NoError(t, sp.WaitReady(handshake))
	time.Sleep(time.Second)
	g := r.top.ts1[0]
	call := func(label string) *callRec {
		return r.playOn(func(p models.Packet) error { return sp.SendDMRD(p) }, id, false,
			voiceCall(label, true, tgID(g), userID(0), 10, r.top.groupTargets(-1, g)), nil)
	}
	before := call("replica-before")
	time.Sleep(time.Second)
	if before != nil && !before.allDelivered() {
		r.res.fail(t, "a call from a repeater on the second replica did not reach the repeaters on the first")
	}
	ctx := context.Background()
	_ = rep.OpenBridgeServer.Stop(ctx)
	_ = rep.IPSCServer.Stop(ctx)
	_ = rep.MMDVMServer.Stop(ctx)
	rep.Hub.Stop(ctx)
	rep.CallTracker.Stop()
	require.NoError(t, sp.Retarget(r.stack.MMDVMAddr))
	start := time.Now()
	for time.Since(start) < 30*time.Second {
		c := call("replica-after")
		time.Sleep(500 * time.Millisecond)
		if c != nil && c.allDelivered() {
			recovery["replica-failover"] = time.Since(start).Seconds()
			return
		}
	}
	recovery["replica-failover"] = -1
	r.res.fail(t, "a session peer of a killed replica got no call through the surviving replica within 30s")
}
