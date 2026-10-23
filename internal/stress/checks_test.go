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
	"strings"
	"time"
)

// checkDelivery fails the area for any missing, duplicate, misrouted or
// altered delivery, and records the totals and the latencies.
func (r *rig) checkDelivery() ledgerTotals {
	const prefix = ""
	tot, samples := r.led.snapshot()
	res := r.res
	res.set(prefix+"calls", float64(tot.Calls))
	res.set(prefix+"bursts_sent", float64(tot.Bursts))
	res.set(prefix+"deliveries_expected", float64(tot.Expected))
	res.set(prefix+"deliveries_ok", float64(tot.Delivered))
	res.set(prefix+"deliveries_missing", float64(tot.Missing))
	res.set(prefix+"deliveries_duplicate", float64(tot.Duplicates))
	res.set(prefix+"deliveries_wrong_target", float64(tot.Wrong))
	res.set(prefix+"bursts_altered", float64(tot.Altered))
	res.set(prefix+"stray_packets", float64(r.led.stray.Load()))
	res.set(prefix+"unidentified_packets", float64(r.led.badKey.Load()))
	res.mu.Lock()
	if res.Latency == nil {
		res.Latency = map[string]latencySummary{}
	}
	res.Latency[prefix+"all"] = r.led.latAll.summary()
	for k, h := range r.led.lat {
		if s := h.summary(); s.Count > 0 {
			res.Latency[prefix+k] = s
		}
	}
	res.mu.Unlock()
	res.extra(prefix+"totals", tot)
	if q := r.queries; q != nil && tot.Bursts > 0 {
		res.set(prefix+"db_queries", float64(q.total.Load()))
		res.set(prefix+"db_queries_per_burst", float64(q.total.Load())/float64(r.led.sent.Load()))
		res.extra(prefix+"db_queries_by_table", q.snapshot())
	}
	res.extra(prefix+"samples", samples)
	if tot.Missing > 0 {
		res.fail(r.t, "%s%d of %d deliveries missing (by kind %v); first: %v", prefix, tot.Missing, tot.Expected, tot.MissingBy, first(samples, 5))
	}
	if tot.Duplicates > 0 {
		res.fail(r.t, "%s%d duplicate deliveries (by kind %v)", prefix, tot.Duplicates, tot.DupBy)
	}
	if tot.Wrong > 0 {
		res.fail(r.t, "%s%d deliveries to targets that must not get them (by kind %v)", prefix, tot.Wrong, tot.WrongBy)
	}
	if tot.Altered > 0 {
		res.fail(r.t, "%s%d bursts arrived altered", prefix, tot.Altered)
	}
	if n := r.led.stray.Load(); n > 0 {
		res.fail(r.t, "%s%d packets of unknown streams arrived", prefix, n)
	}
	return tot
}

func first(s []string, n int) []string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

type callRow struct {
	StreamID      uint
	Kind          string
	SourceID      uint
	DestinationID uint
	GroupCall     bool
	TimeSlot      bool
	IngressID     uint
	Active        bool
	SubKind       string
}

// checkRows checks that each finished call that expects a row has exactly
// one transmission row, ended, with its addresses, slot and kind.
func (r *rig) checkRows(calls []*callRec) {
	want := map[uint]*callRec{}
	var ids []uint
	for _, c := range calls {
		if c.rowKind == "" {
			continue
		}
		want[c.stream] = c
		ids = append(ids, c.stream)
	}
	rows := map[uint][]callRow{}
	for i := 0; i < len(ids); i += 500 {
		var got []callRow
		err := quiet(r.stack.DB).Table("calls").Select("stream_id, kind, sub_kind, source_id, destination_id, group_call, time_slot, ingress_id, active").
			Where("deleted_at IS NULL AND stream_id IN ?", ids[i:min(i+500, len(ids))]).Scan(&got).Error
		if err != nil {
			r.res.fail(r.t, "reading the transmission rows: %v", err)
			return
		}
		for _, g := range got {
			if g.SourceID == parrotID {
				continue
			}
			rows[g.StreamID] = append(rows[g.StreamID], g)
		}
	}
	bad := map[string]int{}
	var samples []string
	note := func(why string, c *callRec, detail string) {
		bad[why]++
		if len(samples) < 10 {
			samples = append(samples, fmt.Sprintf("%s: %s stream %d from %d: %s", why, c.label, c.stream, c.ingress, detail))
		}
	}
	for id, c := range want {
		rs := rows[id]
		switch {
		case len(rs) == 0:
			note("no row", c, "")
			continue
		case len(rs) > 1:
			note("several rows", c, fmt.Sprint(rs))
		}
		g := rs[0]
		if g.Kind != c.rowKind {
			note("wrong kind", c, g.Kind)
		}
		if c.sub != "" && !strings.Contains(g.SubKind, c.sub) {
			note("wrong sub kind", c, g.SubKind)
		}
		if g.SourceID != c.src || g.DestinationID != c.dst || g.GroupCall != c.group || g.TimeSlot != c.slot2 || g.IngressID != c.ingress {
			note("wrong fields", c, fmt.Sprintf("%+v", g))
		}
		if g.Active {
			note("still active", c, "")
		}
	}
	n := 0
	for _, v := range bad {
		n += v
	}
	r.res.set("rows_checked", float64(len(want)))
	r.res.set("rows_bad", float64(n))
	r.res.extra("rows_bad_by_reason", bad)
	if n > 0 {
		r.res.fail(r.t, "%d of %d transmissions have a bad or missing row: %v; first: %v", n, len(want), bad, samples)
	}
	var active int64
	quiet(r.stack.DB).Table("calls").Where("deleted_at IS NULL AND active = ?", true).Count(&active)
	r.res.set("rows_active_at_end", float64(active))
	if active > 0 {
		r.res.fail(r.t, "%d transmission rows are still active at the end", active)
	}
}

// checkNoStuckStreams waits for the hub to count every stream as ended.
func (r *rig) checkNoStuckStreams(within time.Duration) {
	done := make(chan struct{})
	go func() {
		r.stack.Hub.WaitForCalls()
		close(done)
	}()
	start := time.Now()
	select {
	case <-done:
		r.res.set("streams_drained_s", time.Since(start).Seconds())
	case <-time.After(within):
		r.res.fail(r.t, "the hub still has streams in progress %s after the last burst", within)
	}
}

// waitLanes waits for every lane to finish its transmission.
func (r *rig) waitLanes() {
	r.wg.Wait()
}
