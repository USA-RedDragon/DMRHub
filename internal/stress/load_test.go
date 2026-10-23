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
	"math/rand/v2"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
)

// loadMix is mostly voice, with SMS mixed in on the same slots so that the
// latency of data next to voice shows.
func loadMix() mix {
	return mix{
		{55, groupVoice("group", between(17, 83))},
		{20, privateToUser(between(17, 60))},
		{12, func(r *rig, _ *rand.Rand, i int, slot2 bool) script {
			g := r.lanesTG(i, slot2)
			return smsScript("sms-group", true, tgID(g), userID(i), r.groupWant(i, g), sms.FormatIP, enums.DataTypeRate12)
		}},
		{13, func(r *rig, rnd *rand.Rand, i int, _ bool) script {
			j := otherRepeater(rnd, r.top.n, i)
			return smsScript("sms-private", false, userID(j), userID(i), r.privateWant(rptID(j)), sms.FormatIP, enums.DataTypeRate12)
		}},
	}
}

func p99Limit() float64 {
	if v, err := strconv.ParseFloat(os.Getenv("DMRHUB_STRESS_P99_MS"), 64); err == nil && v > 0 {
		return v
	}
	return 50
}

// TestLoadRamp connects the largest number of repeaters of the ramp and
// then makes more and more of them busy, step by step, recording the
// per-burst latency, the losses and the queues of each step, until the
// last step or until the hub degrades: p99 latency over
// DMRHUB_STRESS_P99_MS or any missing delivery.
func TestLoadRamp(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "ramp")
	maxN := 0
	for _, n := range s.Ramp {
		maxN = max(maxN, n)
	}
	r := newRig(t, s, res, logs, rigOptions{repeaters: maxN})
	step := envDuration("DMRHUB_STRESS_STEP", s.Duration)
	limit := p99Limit()
	degraded := 0
	for _, n := range s.Ramp {
		r.led.reset()
		before := r.samp.last()
		q0 := r.queries.total.Load()
		cpu0, t0 := cpuSeconds(), time.Now()
		r.runLanes(context.Background(), step, loadMix(), s.Duty, n, nil)
		r.waitLanes()
		time.Sleep(4 * time.Second)
		r.led.finish(0, true)
		tot, _ := r.led.snapshot()
		after := r.samp.last()
		all, voice, data := r.led.latAll.summary(), r.led.lat["voice"].summary(), r.led.lat["data"].summary()
		row := map[string]float64{
			"repeaters": float64(n), "lanes": float64(2 * n),
			"calls": float64(tot.Calls), "bursts": float64(tot.Bursts),
			"expected": float64(tot.Expected), "missing": float64(tot.Missing), "duplicates": float64(tot.Duplicates), "wrong": float64(tot.Wrong),
			"p50_ms": all.P50, "p90_ms": all.P90, "p99_ms": all.P99, "max_ms": all.Max,
			"voice_p99_ms": voice.P99, "data_p99_ms": data.P99, "data_p50_ms": data.P50, "voice_p50_ms": voice.P50,
			"cpu_per_s":            (cpuSeconds() - cpu0) / time.Since(t0).Seconds(),
			"db_queries_per_burst": float64(r.queries.total.Load()-q0) / float64(max(tot.Bursts, 1)),
			"db_wait_ms":           after["db_wait_ms"] - before["db_wait_ms"],
			"tracker_dropped":      after["tracker_dropped"] - before["tracker_dropped"],
			"tracker_queue":        after["tracker_queue"],
			"pubsub_drops":         float64(logs.count(levelWarn, "Dropping message for slow in-memory subscriber")),
			"loadavg":              loadAvg(),
		}
		res.mu.Lock()
		res.Steps = append(res.Steps, row)
		res.mu.Unlock()
		t.Logf("ramp %d repeaters: p50 %.1f ms p99 %.1f ms max %.1f ms, %d of %d missing, %.1f queries per burst, cpu %.2f",
			n, all.P50, all.P99, all.Max, tot.Missing, tot.Expected, row["db_queries_per_burst"], row["cpu_per_s"])
		if (all.P99 > limit || tot.Missing > 0) && degraded == 0 {
			degraded = n
			res.set("degrades_at_repeaters", float64(n))
		}
		if degraded > 0 && all.P99 > 10*limit {
			break
		}
	}
	cols := []string{"repeaters", "lanes", "calls", "bursts", "expected", "missing", "duplicates", "wrong", "p50_ms", "p90_ms", "p99_ms", "max_ms",
		"voice_p50_ms", "voice_p99_ms", "data_p50_ms", "data_p99_ms", "cpu_per_s", "db_queries_per_burst", "db_wait_ms", "tracker_dropped", "tracker_queue", "pubsub_drops", "loadavg"}
	writeSeries(filepath.Join(s.Results, "ramp.steps.csv"), cols, res.Steps)
	if degraded > 0 {
		res.fail(t, "the hub degrades at %d busy repeaters (%d lanes): p99 over %.0f ms or missing deliveries; see ramp.steps.csv", degraded, 2*degraded, limit)
	}
}

// TestLatency measures the per-burst forwarding time of one call at a time
// on an idle hub, then under the load of every repeater, and checks that
// data bursts do not wait behind voice.
func TestLatency(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "latency")
	r := newRig(t, s, res, logs, rigOptions{})
	limit := p99Limit()

	q0, before := r.queries.total.Load(), r.queries.snapshot()
	idle := 11 * time.Second
	time.Sleep(idle)
	pings := float64(r.top.n) * idle.Seconds() / 5
	res.set("idle_db_queries_per_ping", float64(r.queries.total.Load()-q0)/pings)
	idleQ := map[string]int64{}
	for k, v := range r.queries.snapshot() {
		if d := v - before[k]; d > 0 {
			idleQ[k] = d
		}
	}
	res.extra("idle_db_queries_by_table", idleQ)

	deadline := time.Now().Add(s.Duration / 2)
	rnd := rand.New(rand.NewPCG(1, 1)) //nolint:gosec
	for time.Now().Before(deadline) {
		i := rnd.IntN(r.top.n)
		if rnd.IntN(3) == 0 {
			j := otherRepeater(rnd, r.top.n, i)
			r.play(i, false, smsScript("sms-private", false, userID(j), userID(i), r.privateWant(rptID(j)), sms.FormatIP, enums.DataTypeRate12))
			continue
		}
		r.play(i, false, voiceCall("group", true, tgID(r.top.ts1[i]), userID(i), 20, r.groupWant(i, r.top.ts1[i])))
	}
	time.Sleep(3 * time.Second)
	r.led.finish(0, true)
	quietAll := r.led.latAll.summary()
	idleVoice, idleData := r.led.lat["voice"].summary(), r.led.lat["data"].summary()
	res.mu.Lock()
	res.Latency = map[string]latencySummary{"idle_all": quietAll, "idle_voice": idleVoice, "idle_data": idleData}
	res.mu.Unlock()
	idleTot, _ := r.led.snapshot()

	r.led.reset()
	r.runLanes(context.Background(), s.Duration/2, loadMix(), s.Duty, 0, nil)
	r.waitLanes()
	time.Sleep(4 * time.Second)
	r.led.finish(0, true)
	loaded := r.led.latAll.summary()
	voice, data := r.led.lat["voice"].summary(), r.led.lat["data"].summary()
	res.mu.Lock()
	res.Latency["loaded_all"], res.Latency["loaded_voice"], res.Latency["loaded_data"] = loaded, voice, data
	res.mu.Unlock()
	tot, _ := r.led.snapshot()
	res.set("idle_missing", float64(idleTot.Missing))
	res.set("loaded_missing", float64(tot.Missing))
	res.set("loaded_lanes", float64(2*r.top.n))
	t.Logf("one call at a time: %+v; loaded (%d lanes): voice %+v data %+v", quietAll, 2*r.top.n, voice, data)
	if quietAll.P99 > limit {
		res.fail(t, "p99 latency of one call at a time %.1f ms is over %.0f ms", quietAll.P99, limit)
	}
	if loaded.P99 > limit {
		res.fail(t, "p99 latency under load %.1f ms is over %.0f ms", loaded.P99, limit)
	}
	if data.Count > 0 && data.P99 > 1.5*voice.P99+5 {
		res.fail(t, "data waits behind voice: data p99 %.1f ms, voice p99 %.1f ms", data.P99, voice.P99)
	}
	if idleTot.Missing+tot.Missing > 0 {
		res.fail(t, "%d deliveries missing at idle and %d under load", idleTot.Missing, tot.Missing)
	}
}

// TestSoak runs the call mix for the whole duration, finishing the calls
// as it goes, and checks that memory, goroutines, file descriptors, DB
// connections and the queues stay flat, that every burst arrived, and
// that the error log does not grow.
func TestSoak(t *testing.T) { //nolint:paralleltest
	s, res, logs := area(t, "soak")
	r := newRig(t, s, res, logs, rigOptions{withOB: true})
	col := &collector{}
	r.led.onDone = col.add
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		tick := time.NewTicker(5 * time.Second)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-tick.C:
				r.led.finish(10*time.Second, false)
			}
		}
	}()
	r.runLanes(ctx, s.Duration, callMix(), s.Duty, 0, nil)
	r.waitLanes()
	time.Sleep(10 * time.Second)
	cancel()
	r.led.finish(0, true)
	r.checkDelivery()
	r.checkRows(col.all())
	r.checkNoStuckStreams(15 * time.Second)
	r.checkFlat()
}

// checkFlat compares the gauges of the first and the last fifth of the
// run.
func (r *rig) checkFlat() {
	rows := r.samp.finish()
	r.samp = nil
	if len(rows) < 10 {
		r.res.set("flat_samples", float64(len(rows)))
		return
	}
	n := len(rows) / 5
	avg := func(part []map[string]float64, k string) float64 {
		total := 0.0
		for _, row := range part {
			total += row[k]
		}
		return total / float64(len(part))
	}
	head, tail := rows[1:n+1], rows[len(rows)-n:]
	limits := map[string]float64{"goroutines": 1.2, "fds": 1.1, "heap_mb": 1.5, "rss_mb": 1.5, "db_open": 1.5}
	for k, f := range limits {
		a, b := avg(head, k), avg(tail, k)
		r.res.set("flat_"+k+"_first", a)
		r.res.set("flat_"+k+"_last", b)
		if a > 0 && b > a*f+5 {
			r.res.fail(r.t, "%s grows over the run: %.1f in the first fifth, %.1f in the last", k, a, b)
		}
	}
	for _, k := range []string{"tracker_queue", "server_queue"} {
		if b := avg(tail, k); b > 100 {
			r.res.fail(r.t, "%s stays at %.0f at the end of the run", k, b)
		}
	}
	elapsed := tail[len(tail)-1]["t_s"] - head[0]["t_s"]
	errs := tail[len(tail)-1]["errors_logged"] - head[0]["errors_logged"]
	r.res.set("errors_logged_per_min", errs/elapsed*60)
	if errs > 0 {
		r.res.fail(r.t, "%.0f errors logged during the run", errs)
	}
}
