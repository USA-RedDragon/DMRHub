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
	"bufio"
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

// sampleColumns are the columns of <area>.samples.csv, in order.
var sampleColumns = []string{ //nolint:gochecknoglobals
	"t_s", "rss_mb", "heap_mb", "goroutines", "fds", "cpu_s",
	"db_open", "db_in_use", "db_wait_count", "db_wait_ms", "redis_clients",
	"tracker_queue", "tracker_dropped", "undeliverable_total", "messages_dropped",
	"server_queue", "udp_rcvbuf_errors", "hub_socket_drops",
	"sent", "received", "db_queries", "calls_rows", "messages_rows", "errors_logged", "warnings_logged",
}

// sampler records the resource and queue gauges once a second.
type sampler struct {
	stack   *testutils.IntegrationStack
	path    string
	start   time.Time
	extra   func() map[string]float64
	logs    *logCounter
	redis   *redis.Client
	ports   []int
	stop    chan struct{}
	done    chan struct{}
	mu      sync.Mutex
	rows    []map[string]float64
	started bool
}

func newSampler(stack *testutils.IntegrationStack, path string, logs *logCounter, extra func() map[string]float64) *sampler {
	s := &sampler{stack: stack, path: path, start: time.Now(), extra: extra, logs: logs, stop: make(chan struct{}), done: make(chan struct{})}
	if stack.Config.Redis.Enabled {
		s.redis = redis.NewClient(&redis.Options{Addr: fmt.Sprintf("%s:%d", stack.Config.Redis.Host, stack.Config.Redis.Port)})
	}
	for _, a := range []string{stack.MMDVMAddr, stack.IPSCAddr, stack.OpenBridgeAddr} {
		if i := strings.LastIndexByte(a, ':'); i >= 0 {
			if p, err := strconv.Atoi(a[i+1:]); err == nil {
				s.ports = append(s.ports, p)
			}
		}
	}
	return s
}

func (s *sampler) run(every time.Duration) {
	s.started = true
	go func() {
		defer close(s.done)
		tick := time.NewTicker(every)
		defer tick.Stop()
		s.sample()
		for {
			select {
			case <-s.stop:
				s.sample()
				return
			case <-tick.C:
				s.sample()
			}
		}
	}()
}

// finish stops the sampler and writes the CSV.
func (s *sampler) finish() []map[string]float64 {
	if !s.started {
		return nil
	}
	close(s.stop)
	<-s.done
	if s.redis != nil {
		_ = s.redis.Close()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if f, err := os.Create(s.path); err == nil {
		w := csv.NewWriter(f)
		_ = w.Write(sampleColumns)
		for _, r := range s.rows {
			rec := make([]string, len(sampleColumns))
			for i, c := range sampleColumns {
				rec[i] = strconv.FormatFloat(r[c], 'f', -1, 64)
			}
			_ = w.Write(rec)
		}
		w.Flush()
		_ = f.Close()
	}
	return s.rows
}

func (s *sampler) last() map[string]float64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.rows) == 0 {
		return map[string]float64{}
	}
	return s.rows[len(s.rows)-1]
}

func (s *sampler) sample() {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	r := map[string]float64{
		"t_s":        time.Since(s.start).Seconds(),
		"rss_mb":     float64(rssBytes()) / (1 << 20),
		"heap_mb":    float64(ms.HeapInuse) / (1 << 20),
		"goroutines": float64(runtime.NumGoroutine()),
		"fds":        float64(openFDs()),
		"cpu_s":      cpuSeconds(),
	}
	if sqlDB, err := s.stack.DB.DB(); err == nil {
		st := sqlDB.Stats()
		r["db_open"] = float64(st.OpenConnections)
		r["db_in_use"] = float64(st.InUse)
		r["db_wait_count"] = float64(st.WaitCount)
		r["db_wait_ms"] = float64(st.WaitDuration.Milliseconds())
	}
	if s.redis != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		if info, err := s.redis.Info(ctx, "clients").Result(); err == nil {
			r["redis_clients"] = infoField(info, "connected_clients") - 1
		} else {
			r["redis_clients"] = -1
		}
		cancel()
	}
	r["tracker_queue"] = float64(trackerQueue(s.stack))
	r["tracker_dropped"] = float64(s.stack.CallTracker.Dropped())
	r["undeliverable_total"] = counterValue("dmr_undeliverable_bursts_total")
	r["messages_dropped"] = counterValue("message_store_dropped_total")
	r["server_queue"] = float64(serverQueues(s.stack))
	r["udp_rcvbuf_errors"] = udpRcvbufErrors()
	r["hub_socket_drops"] = float64(socketDrops(s.ports))
	var n int64
	if quiet(s.stack.DB).Table("calls").Count(&n).Error == nil {
		r["calls_rows"] = float64(n)
	}
	if quiet(s.stack.DB).Table("messages").Count(&n).Error == nil {
		r["messages_rows"] = float64(n)
	}
	if s.logs != nil {
		r["errors_logged"] = float64(s.logs.total(levelError))
		r["warnings_logged"] = float64(s.logs.total(levelWarn))
	}
	if s.extra != nil {
		for k, v := range s.extra() {
			r[k] = v
		}
	}
	s.mu.Lock()
	s.rows = append(s.rows, r)
	s.mu.Unlock()
}

func rssBytes() int64 {
	f, err := os.Open("/proc/self/status")
	if err != nil {
		return 0
	}
	defer func() { _ = f.Close() }()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		if v, ok := strings.CutPrefix(sc.Text(), "VmRSS:"); ok {
			kb, _ := strconv.ParseInt(strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(v), "kB")), 10, 64)
			return kb << 10
		}
	}
	return 0
}

func openFDs() int {
	ents, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return -1
	}
	return len(ents)
}

func cpuSeconds() float64 {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0
	}
	tv := func(t syscall.Timeval) float64 { return float64(t.Sec) + float64(t.Usec)/1e6 }
	return tv(ru.Utime) + tv(ru.Stime)
}

func infoField(info, name string) float64 {
	for _, line := range strings.Split(info, "\n") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(line), name+":"); ok {
			f, _ := strconv.ParseFloat(strings.TrimSpace(v), 64)
			return f
		}
	}
	return -1
}

// trackerQueue is the number of packets queued on the call tracker
// workers. The tracker has no accessor for it, so it reads the length of
// the worker channels by reflection.
func trackerQueue(stack *testutils.IntegrationStack) int {
	v := reflect.ValueOf(stack.CallTracker).Elem().FieldByName("shards")
	if !v.IsValid() {
		return -1
	}
	n := 0
	for i := range v.Len() {
		in := v.Index(i).Elem().FieldByName("in")
		if in.IsValid() {
			n += in.Len()
		}
	}
	return n
}

// serverQueues is the number of packets waiting on the channels of the
// MMDVM and OpenBridge servers: the ingress and egress queues and the
// channel from the hub. It reads them by reflection.
func serverQueues(stack *testutils.IntegrationStack) int {
	n := 0
	for _, srv := range []any{stack.MMDVMServer, stack.OpenBridgeServer} {
		n += chanLens(reflect.ValueOf(srv).Elem(), "incomingChan", "outgoingChan")
		if h := reflect.ValueOf(srv).Elem().FieldByName("hubHandle"); h.IsValid() && !h.IsNil() {
			n += chanLens(h.Elem(), "Packets")
		}
	}
	return n
}

func chanLens(v reflect.Value, names ...string) int {
	n := 0
	for _, name := range names {
		if f := v.FieldByName(name); f.IsValid() && f.Kind() == reflect.Chan && !f.IsNil() {
			n += f.Len()
		}
	}
	return n
}

func counterValue(name string) float64 {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return -1
	}
	total := 0.0
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			total += metricValue(m)
		}
	}
	return total
}

func counterWith(name, label, value string) float64 {
	mfs, err := prometheus.DefaultGatherer.Gather()
	if err != nil {
		return -1
	}
	total := 0.0
	for _, mf := range mfs {
		if mf.GetName() != name {
			continue
		}
		for _, m := range mf.GetMetric() {
			for _, l := range m.GetLabel() {
				if l.GetName() == label && l.GetValue() == value {
					total += metricValue(m)
				}
			}
		}
	}
	return total
}

func metricValue(m *dto.Metric) float64 {
	if c := m.GetCounter(); c != nil {
		return c.GetValue()
	}
	if g := m.GetGauge(); g != nil {
		return g.GetValue()
	}
	return 0
}

// udpRcvbufErrors is RcvbufErrors of /proc/net/snmp. It counts every UDP
// socket of the network namespace, the clients of the test included.
func udpRcvbufErrors() float64 {
	b, err := os.ReadFile("/proc/net/snmp")
	if err != nil {
		return -1
	}
	var head []string
	for _, line := range strings.Split(string(b), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || f[0] != "Udp:" {
			continue
		}
		if head == nil {
			head = f
			continue
		}
		for i, h := range head {
			if h == "RcvbufErrors" && i < len(f) {
				v, _ := strconv.ParseFloat(f[i], 64)
				return v
			}
		}
	}
	return -1
}

// socketDrops sums the drops column of /proc/net/udp for the sockets bound
// to ports, the listening sockets of the hub.
func socketDrops(ports []int) int64 {
	b, err := os.ReadFile("/proc/net/udp")
	if err != nil {
		return -1
	}
	want := map[string]bool{}
	for _, p := range ports {
		want[fmt.Sprintf("%04X", p)] = true
	}
	var n int64
	for _, line := range strings.Split(string(b), "\n")[1:] {
		f := strings.Fields(line)
		if len(f) < 13 {
			continue
		}
		local := f[1]
		i := strings.IndexByte(local, ':')
		if i < 0 || !want[local[i+1:]] {
			continue
		}
		d, _ := strconv.ParseInt(f[len(f)-1], 10, 64)
		n += d
	}
	return n
}

// writeSeries writes rows as a CSV with the given columns.
func writeSeries(path string, cols []string, rows []map[string]float64) {
	f, err := os.Create(filepath.Clean(path))
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }()
	w := csv.NewWriter(f)
	_ = w.Write(cols)
	for _, r := range rows {
		rec := make([]string, len(cols))
		for i, c := range cols {
			rec[i] = strconv.FormatFloat(r[c], 'f', -1, 64)
		}
		_ = w.Write(rec)
	}
	w.Flush()
}

type quietKey struct{}

// quiet is db for the statements of the test, which the query counter
// leaves out.
func quiet(db *gorm.DB) *gorm.DB {
	return db.WithContext(context.WithValue(context.Background(), quietKey{}, true))
}

// queryCounter counts the statements that the hub runs, by operation and
// table, through gorm callbacks.
type queryCounter struct {
	total atomic.Int64
	by    sync.Map
}

func countQueries(db *gorm.DB) *queryCounter {
	q := &queryCounter{}
	hook := func(op string) func(*gorm.DB) {
		return func(tx *gorm.DB) {
			if ctx := tx.Statement.Context; ctx != nil && ctx.Value(quietKey{}) != nil {
				return
			}
			q.total.Add(1)
			key := op + " " + tx.Statement.Table
			v, _ := q.by.LoadOrStore(key, &atomic.Int64{})
			v.(*atomic.Int64).Add(1) //nolint:forcetypeassert
		}
	}
	cb := db.Callback()
	_ = cb.Query().After("gorm:query").Register("stress:count", hook("select"))
	_ = cb.Create().After("gorm:create").Register("stress:count", hook("insert"))
	_ = cb.Update().After("gorm:update").Register("stress:count", hook("update"))
	_ = cb.Delete().After("gorm:delete").Register("stress:count", hook("delete"))
	_ = cb.Row().After("gorm:row").Register("stress:count", hook("row"))
	_ = cb.Raw().After("gorm:raw").Register("stress:count", hook("raw"))
	return q
}

func (q *queryCounter) snapshot() map[string]int64 {
	out := map[string]int64{}
	q.by.Range(func(k, v any) bool {
		out[k.(string)] = v.(*atomic.Int64).Load() //nolint:forcetypeassert
		return true
	})
	return out
}
