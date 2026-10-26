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
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
)

const (
	rptPassword = "s3cret"
	ipscKey     = "0000000000000000000000000000000000003039"
	obPassword  = "bridge-secret"
	handshake   = 10 * time.Second
	levelError  = slog.LevelError
	levelWarn   = slog.LevelWarn
)

// settings are the knobs of a run, from the environment.
type settings struct {
	Duration  time.Duration `json:"duration_ns"`
	Repeaters int           `json:"repeaters"`
	Fanout    int           `json:"fanout"`
	Duty      float64       `json:"duty"`
	Backend   string        `json:"backend"`
	Ramp      []int         `json:"ramp"`
	Long      time.Duration `json:"long_call_ns"`
	Results   string        `json:"-"`
	Profile   bool          `json:"profile"`
	Ext       string        `json:"ext"`
}

func envInt(name string, def int) int {
	if v, err := strconv.Atoi(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func envDuration(name string, def time.Duration) time.Duration {
	if v, err := time.ParseDuration(os.Getenv(name)); err == nil && v > 0 {
		return v
	}
	return def
}

func loadSettings(t *testing.T) settings {
	t.Helper()
	s := settings{
		Duration:  envDuration("DMRHUB_STRESS_DURATION", 20*time.Second),
		Repeaters: envInt("DMRHUB_STRESS_REPEATERS", 20),
		Fanout:    envInt("DMRHUB_STRESS_FANOUT", 5),
		Duty:      0.6,
		Backend:   os.Getenv("DMRHUB_STRESS_BACKEND"),
		Long:      envDuration("DMRHUB_STRESS_LONG_CALL", 0),
		Results:   os.Getenv("DMRHUB_STRESS_RESULTS"),
		Profile:   os.Getenv("DMRHUB_STRESS_PPROF") != "",
		Ext:       os.Getenv("DMRHUB_STRESS_EXT_DIR"),
	}
	if v, err := strconv.ParseFloat(os.Getenv("DMRHUB_STRESS_DUTY"), 64); err == nil && v > 0 && v <= 1 {
		s.Duty = v
	}
	if s.Backend == "" {
		s.Backend = "sqlite"
	}
	for _, f := range strings.Split(os.Getenv("DMRHUB_STRESS_RAMP"), ",") {
		if n, err := strconv.Atoi(strings.TrimSpace(f)); err == nil && n > 0 {
			s.Ramp = append(s.Ramp, n)
		}
	}
	if len(s.Ramp) == 0 {
		s.Ramp = []int{10, 25, 50, 100}
	}
	if s.Results == "" {
		s.Results = filepath.Join("..", "..", "artifacts", "stress", "latest")
	}
	require.NoError(t, os.MkdirAll(s.Results, 0o750))
	return s
}

// result is what an area writes to <results>/<area>.json.
type result struct {
	Area     string                     `json:"area"`
	Backend  string                     `json:"backend"`
	Settings settings                   `json:"settings"`
	Started  time.Time                  `json:"started"`
	Elapsed  float64                    `json:"elapsed_s"`
	Passed   bool                       `json:"passed"`
	Numbers  map[string]float64         `json:"numbers"`
	Latency  map[string]latencySummary  `json:"latency_ms,omitempty"`
	Steps    []map[string]float64       `json:"steps,omitempty"`
	Failures []string                   `json:"failures"`
	Logs     map[string]map[string]int  `json:"logs"`
	Extra    map[string]json.RawMessage `json:"extra,omitempty"`

	mu sync.Mutex
}

func (r *result) set(name string, v float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Numbers[name] = v
}

func (r *result) inc(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Numbers[name]++
}

// fail records a failure of the pass criteria. The test fails at the end,
// after the results are written, so one failure does not hide the next.
func (r *result) fail(t *testing.T, format string, args ...any) {
	t.Helper()
	msg := fmt.Sprintf(format, args...)
	r.mu.Lock()
	r.Failures = append(r.Failures, msg)
	r.mu.Unlock()
	t.Error(msg)
}

func (r *result) extra(name string, v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.Extra == nil {
		r.Extra = map[string]json.RawMessage{}
	}
	r.Extra[name] = b
}

// area starts an area: it loads the settings, routes the logs through a
// counter, starts the profiles when asked, and writes the result at the
// end of the test.
func area(t *testing.T, name string) (settings, *result, *logCounter) {
	t.Helper()
	s := loadSettings(t)
	res := &result{Area: name, Backend: s.Backend, Settings: s, Started: time.Now(), Numbers: map[string]float64{}, Failures: []string{}}
	res.Numbers["loadavg_start"] = loadAvg()
	logs := newLogCounter(filepath.Join(s.Results, name+".log"))
	prev := slog.Default()
	slog.SetDefault(slog.New(logs))
	var cpu *os.File
	if s.Profile {
		runtime.SetMutexProfileFraction(5)
		runtime.SetBlockProfileRate(int(time.Millisecond))
		f, err := os.Create(filepath.Join(s.Results, name+".cpu.pprof"))
		if err == nil && pprof.StartCPUProfile(f) == nil {
			cpu = f
		}
	}
	t.Cleanup(func() {
		if cpu != nil {
			pprof.StopCPUProfile()
			_ = cpu.Close()
			for _, p := range []string{"heap", "goroutine", "mutex", "block"} {
				if f, err := os.Create(filepath.Join(s.Results, name+"."+p+".pprof")); err == nil {
					_ = pprof.Lookup(p).WriteTo(f, 0)
					_ = f.Close()
				}
			}
		}
		slog.SetDefault(prev)
		logs.close()
		res.Elapsed = time.Since(res.Started).Seconds()
		res.Numbers["loadavg_end"] = loadAvg()
		res.Passed = !t.Failed() && len(res.Failures) == 0
		res.Logs = logs.summary()
		b, err := json.MarshalIndent(res, "", "  ")
		if err == nil {
			_ = os.WriteFile(filepath.Join(s.Results, name+".json"), append(b, '\n'), 0o600)
		}
		t.Logf("%s: results in %s", name, s.Results)
	})
	return s, res, logs
}

// newStack makes the integration stack of the backend that the settings
// name: sqlite is a SQLite file with the default parameters, as a
// production install has, sqlite-memory the shared in-memory database of
// the integration tests, postgres-redis Postgres and Redis in Docker, and
// mysql and mariadb MySQL or MariaDB in Docker.
// The containers of a Postgres and Redis backend are in c.
func newStack(t *testing.T, s settings) (*testutils.IntegrationStack, *testutils.Containers) {
	t.Helper()
	c := &testutils.Containers{}
	switch s.Backend {
	case "sqlite":
		dir := dataDir(t, s)
		return testutils.SetupIntegrationStack(t, testutils.Backend{Name: "sqlite-file", Setup: func(_ *testing.T, cfg *config.Config) {
			def, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(t, err)
			cfg.Database.Database = filepath.Join(dir, "DMRHub.db")
			cfg.Database.ExtraParameters = def.Database.ExtraParameters
		}}), c
	case "sqlite-memory":
		return testutils.SetupIntegrationStack(t), c
	case "postgres-redis":
		return testutils.SetupIntegrationStack(t, testutils.PostgresRedisBackendInto(c)), c
	case "mysql":
		return testutils.SetupIntegrationStack(t, testutils.MySQLBackend()), c
	case "mariadb":
		return testutils.SetupIntegrationStack(t, testutils.MariaDBBackend()), c
	}
	t.Fatalf("unknown DMRHUB_STRESS_BACKEND %q (sqlite, sqlite-memory, postgres-redis, mysql or mariadb)", s.Backend)
	return nil, nil
}

// step runs fn as the named part of an area. The parts share the rig and
// run one after another.
func step(t *testing.T, name string, fn func(t *testing.T)) {
	t.Helper()
	t.Run(name, fn)
}

// loadAvg is the one-minute load average of the machine, which other work
// on a shared machine raises.
func loadAvg() float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return -1
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return -1
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	return v
}

// dataDir is a fresh directory for the files of the hub, under
// DMRHUB_STRESS_DATA or else the results directory, never /tmp. The test
// removes it at the end.
func dataDir(t *testing.T, s settings) string {
	t.Helper()
	base := os.Getenv("DMRHUB_STRESS_DATA")
	if base == "" {
		base = filepath.Join(s.Results, "data")
	}
	require.NoError(t, os.MkdirAll(base, 0o750))
	dir, err := os.MkdirTemp(base, "hub-")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// latencySummary is the distribution of a set of durations, in
// milliseconds.
type latencySummary struct {
	Count int     `json:"count"`
	P50   float64 `json:"p50"`
	P90   float64 `json:"p90"`
	P99   float64 `json:"p99"`
	P999  float64 `json:"p999"`
	Max   float64 `json:"max"`
}

// histogram keeps every sample, up to a cap, then a uniform reservoir.
type histogram struct {
	mu   sync.Mutex
	vals []time.Duration
	n    int
	max  time.Duration
	seed uint64
}

const histogramCap = 2_000_000

func (h *histogram) add(d time.Duration) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.n++
	if d > h.max {
		h.max = d
	}
	if len(h.vals) < histogramCap {
		h.vals = append(h.vals, d)
		return
	}
	h.seed = h.seed*6364136223846793005 + 1442695040888963407
	if i := int(h.seed>>33) % h.n; i < histogramCap {
		h.vals[i] = d
	}
}

func (h *histogram) summary() latencySummary {
	h.mu.Lock()
	vals := append([]time.Duration(nil), h.vals...)
	n, maxV := h.n, h.max
	h.mu.Unlock()
	if len(vals) == 0 {
		return latencySummary{}
	}
	sort.Slice(vals, func(i, j int) bool { return vals[i] < vals[j] })
	q := func(p float64) float64 {
		i := int(p * float64(len(vals)-1))
		return ms(vals[i])
	}
	return latencySummary{Count: n, P50: q(0.5), P90: q(0.9), P99: q(0.99), P999: q(0.999), Max: ms(maxV)}
}

func (h *histogram) reset() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.vals, h.n, h.max = h.vals[:0], 0, 0
}

func ms(d time.Duration) float64 { return float64(d.Microseconds()) / 1000 }

// logCounter is a slog handler that counts records by level and message
// and writes them, as text, to a file.
type logCounter struct {
	mu     sync.Mutex
	counts map[string]map[string]int
	f      *os.File
	inner  slog.Handler
}

func newLogCounter(path string) *logCounter {
	l := &logCounter{counts: map[string]map[string]int{}}
	if f, err := os.Create(path); err == nil {
		l.f = f
		l.inner = slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})
	}
	return l
}

func (l *logCounter) Enabled(_ context.Context, lv slog.Level) bool { return lv >= slog.LevelInfo }

func (l *logCounter) Handle(ctx context.Context, r slog.Record) error {
	lv := r.Level.String()
	l.mu.Lock()
	if l.counts[lv] == nil {
		l.counts[lv] = map[string]int{}
	}
	l.counts[lv][r.Message]++
	n := l.counts[lv][r.Message]
	inner := l.inner
	l.mu.Unlock()
	if inner == nil || (r.Level < slog.LevelWarn && n > 200) || n > 2000 {
		return nil
	}
	if err := inner.Handle(ctx, r); err != nil {
		return fmt.Errorf("writing log: %w", err)
	}
	return nil
}

func (l *logCounter) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logView{l: l, attrs: attrs}
}

func (l *logCounter) WithGroup(string) slog.Handler { return l }

func (l *logCounter) count(level slog.Level, msg string) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.counts[level.String()][msg]
}

func (l *logCounter) total(level slog.Level) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := 0
	for _, c := range l.counts[level.String()] {
		n += c
	}
	return n
}

func (l *logCounter) summary() map[string]map[string]int {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := map[string]map[string]int{}
	for lv, m := range l.counts {
		if lv == slog.LevelInfo.String() || lv == slog.LevelDebug.String() {
			continue
		}
		out[lv] = map[string]int{}
		for k, v := range m {
			out[lv][k] = v
		}
	}
	return out
}

func (l *logCounter) close() {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f, l.inner = nil, nil
	}
}

type logView struct {
	l     *logCounter
	attrs []slog.Attr
}

func (v *logView) Enabled(ctx context.Context, lv slog.Level) bool { return v.l.Enabled(ctx, lv) }

func (v *logView) Handle(ctx context.Context, r slog.Record) error {
	r.AddAttrs(v.attrs...)
	return v.l.Handle(ctx, r)
}

func (v *logView) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &logView{l: v.l, attrs: append(append([]slog.Attr(nil), v.attrs...), attrs...)}
}

func (v *logView) WithGroup(string) slog.Handler { return v }
