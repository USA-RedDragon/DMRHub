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

package inside_test

import (
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/builtin"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/inside"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var guests struct { //nolint:gochecknoglobals // built once for all the tests
	once sync.Once
	dir  string
	err  error
}

func guest(t *testing.T, name string) []byte {
	t.Helper()
	guests.once.Do(buildGuests)
	require.NoError(t, guests.err)
	b, err := os.ReadFile(filepath.Join(guests.dir, name+".wasm"))
	require.NoError(t, err)
	return b
}

func buildGuests() {
	guests.dir, guests.err = os.MkdirTemp("", "dmrhub-inside-guests-")
	if guests.err != nil {
		return
	}
	for _, g := range []string{"echo", "netprobe", "quit"} {
		cmd := exec.Command("go", "build", "-o", filepath.Join(guests.dir, g+".wasm"), "../wasm/testdata/"+g) //nolint:noctx // a build for the tests
		cmd.Env = append(os.Environ(), "GOOS=wasip1", "GOARCH=wasm")
		if out, err := cmd.CombinedOutput(); err != nil {
			guests.err = fmt.Errorf("could not build %s: %w\n%s", g, err, out)
			return
		}
	}
}

// compileGuests builds the guests and compiles them and the built-in
// module into engine before the tests run, so that no wait of a test
// includes a build or a compile.
func compileGuests() {
	guests.once.Do(buildGuests)
	if guests.err != nil {
		return
	}
	ctx := context.Background()
	for _, g := range []string{"echo", "netprobe", "quit"} {
		b, err := os.ReadFile(filepath.Join(guests.dir, g+".wasm"))
		if err != nil {
			return
		}
		_, _ = engine.Inspect(ctx, b)
	}
	for _, p := range builtin.All()[:1] {
		module, err := p.Module()
		if err != nil {
			return
		}
		if inst, err := engine.Start(ctx, p.Key, module, nil, p.Args()...); err == nil {
			inst.Stop()
		}
	}
}

func TestMain(m *testing.M) {
	compileGuests()
	code := m.Run()
	if guests.dir != "" {
		_ = os.RemoveAll(guests.dir)
	}
	os.Exit(code)
}

// engine runs the plugins of the tests with no limit on CPU, which a test
// under the race detector, in wazero's interpreter, would reach.
var engine = wasm.NewCachedEngine(func() wasm.Limits { //nolint:gochecknoglobals // one compilation cache for the tests
	l := wasm.DefaultLimits()
	l.CPUShare = 100
	return l
}())

type routed struct {
	mu   sync.Mutex
	pkts []models.Packet
}

func (r *routed) RoutePacket(_ context.Context, p models.Packet, _ string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pkts = append(r.pkts, p)
}

func (r *routed) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.pkts)
}

func (r *routed) last() models.Packet {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.pkts[len(r.pkts)-1]
}

// cluster is the database, pubsub and KV store that the replicas of a
// test share.
type cluster struct {
	db    *gorm.DB
	ps    pubsub.PubSub
	kv    kv.KV
	clock clock.Clock
}

func (c *cluster) now() time.Time {
	return clock.Or(c.clock).Now()
}

func newCluster(t *testing.T) *cluster {
	t.Helper()
	return newClusterOn(t, clock.NewFake(time.Now()))
}

func newClusterOn(t *testing.T, c clock.Clock) *cluster {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = filepath.Join(t.TempDir(), "dmrhub.db")
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.Background(), &cfg, kv.WithClock(c), kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = store.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return &cluster{db: database, ps: ps, kv: store, clock: c}
}

// replica is one DMRHub replica of a cluster, which runs plugins inside.
type replica struct {
	m        *inside.Manager
	registry *plugins.Registry
	svc      *plugins.Service
	router   *routed
}

func fast() inside.Timing {
	return inside.Timing{Renew: 200 * time.Millisecond, Lease: time.Second, Quiet: 500 * time.Millisecond, MaxWait: 10 * time.Second,
		MinBackoff: 100 * time.Millisecond, MaxBackoff: time.Second}
}

// engineOn returns e with its limits timed on c, so that the hello and
// answer deadlines of a guest step with the test clock rather than run out
// on the real clock while the guest is slow to start.
func engineOn(e *wasm.Engine, c clock.Clock) *wasm.Engine {
	l := e.Limits()
	l.Clock = c
	return e.Limited(l)
}

func (c *cluster) replica(t *testing.T, name string, e *wasm.Engine, on bool) *replica {
	t.Helper()
	e = engineOn(e, c.clock)
	registry, err := plugins.NewRegistry(c.db, c.ps)
	require.NoError(t, err)
	done := make(chan struct{})
	registry.Follow(done)
	r := &replica{registry: registry, router: &routed{}}
	r.svc = plugins.NewService(c.db, c.ps, registry, r.router, nil, plugins.WithClock(c.clock))
	r.m = inside.NewManager(inside.Deps{
		DB: c.db, PubSub: c.ps, KV: c.kv, Instance: name, Version: "v1.3.4", Service: r.svc, Registry: registry, Engine: e, Timing: fast(),
		Clock: c.clock,
	})
	require.NoError(t, r.m.Start(context.Background(), on))
	t.Cleanup(func() {
		r.m.Stop(context.Background())
		close(done)
	})
	return r
}

func online(t *testing.T, c *cluster, id uint) bool {
	t.Helper()
	p, err := models.FindPlugin(c.db, id)
	return err == nil && p.Online(c.now())
}

func builtinOf(t *testing.T, c *cluster, key string) models.Plugin {
	t.Helper()
	list, err := models.ListPlugins(c.db)
	require.NoError(t, err)
	for _, p := range list {
		if p.Kind == models.PluginBuiltin && p.Builtin == key {
			return p
		}
	}
	t.Fatalf("no built-in plugin %s", key)
	return models.Plugin{}
}

func upload(t *testing.T, c *cluster, name string, module []byte, ranges ...models.PluginRange) models.Plugin {
	t.Helper()
	p, err := models.CreateUploadedPlugin(c.db, models.PluginInfo{Name: name}, ranges, nil, module, time.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	return p
}

func TestTheBuiltinPluginsRunWithTheirIDs(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	r := c.replica(t, "a", engine, true)
	parrot := builtinOf(t, c, "parrot")
	locator := builtinOf(t, c, "locator")
	assert.Equal(t, "Parrot", parrot.Name)
	assert.Equal(t, "v1.3.4", parrot.Version)
	require.Len(t, parrot.Ranges, 1)
	assert.Equal(t, uint(9990), parrot.Ranges[0].First)
	assert.Equal(t, "Location server", locator.Name)
	require.Len(t, locator.Ranges, 1)
	assert.Equal(t, uint(5050), locator.Ranges[0].First)
	assert.Equal(t, uint(5059), locator.Ranges[0].Last)
	require.Eventually(t, func() bool { return online(t, c, parrot.ID) && online(t, c, locator.ID) }, 60*time.Second, 50*time.Millisecond)
	owner, ok := r.registry.Owner(9990)
	require.True(t, ok)
	assert.Equal(t, parrot.ID, owner)
}

func watchPasses(r *replica) <-chan time.Time {
	ch := make(chan time.Time, 1024)
	inside.OnReconcile(r.m, func(at time.Time) {
		select {
		case ch <- at:
		default:
		}
	})
	return ch
}

func passedSince(t *testing.T, passes <-chan time.Time, at time.Time) {
	t.Helper()
	wait := inside.DefaultTiming().Lease
	timer := time.NewTimer(wait)
	defer timer.Stop()
	for {
		select {
		case got := <-passes:
			if !got.Before(at) {
				return
			}
		case <-timer.C:
			t.Fatalf("no pass of the reconcile started at %v or later within the lease of %v", at, wait)
		}
	}
}

func TestNothingRunsInsideWhenItIsOff(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	r := c.replica(t, "a", engine, false)
	passes := watchPasses(r)
	list, err := models.ListPlugins(c.db)
	require.NoError(t, err)
	assert.Empty(t, list, "no built-in plugins are made")
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9100, Last: 9100})
	fake.Advance(fast().Renew)
	passedSince(t, passes, fake.Now())
	assert.Empty(t, r.m.Running())
	assert.False(t, online(t, c, p.ID))
	_, ok := r.registry.Owner(9100)
	assert.False(t, ok, "nobody gets the calls to a plugin inside DMRHub that does not run")

	require.NoError(t, r.m.SetOn(context.Background(), true))
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 30*time.Second, 50*time.Millisecond)
	_, ok = r.registry.Owner(9100)
	assert.True(t, ok)
	require.NoError(t, r.m.SetOn(context.Background(), false))
	require.Eventually(t, func() bool { return len(r.m.Running()) == 0 && !online(t, c, p.ID) }, 30*time.Second, 50*time.Millisecond)
}

func TestOnlyOneReplicaRunsAPlugin(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	a := c.replica(t, "a", engine, true)
	b := c.replica(t, "b", engine, true)
	passesA, passesB := watchPasses(a), watchPasses(b)
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9100, Last: 9100})
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 60*time.Second, 50*time.Millisecond)
	timing := fast()
	for step := time.Duration(0); step < 3*timing.Lease; step += timing.Renew {
		fake.Advance(timing.Renew)
		passedSince(t, passesA, fake.Now())
		passedSince(t, passesB, fake.Now())
		_, onA := a.m.Running()[p.ID]
		_, onB := b.m.Running()[p.ID]
		require.True(t, onA != onB, "the plugin runs on exactly one replica at %v: a %v, b %v", step, onA, onB)
	}
	first, other := a, b
	if _, onB := b.m.Running()[p.ID]; onB {
		first, other = b, a
	}
	first.m.Stop(context.Background())
	require.Eventually(t, func() bool {
		_, ok := other.m.Running()[p.ID]
		return ok && online(t, c, p.ID)
	}, 30*time.Second, 50*time.Millisecond, "the other replica takes the plugin over")
}

func TestADrainHandsThePluginsInsideToAnotherReplica(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	a := c.replica(t, "a", engine, true)
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9110, Last: 9110})
	require.Eventually(t, func() bool {
		_, ok := a.m.Running()[p.ID]
		return ok && online(t, c, p.ID)
	}, 60*time.Second, 50*time.Millisecond)
	b := c.replica(t, "b", engine, true)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(cfg.DMR.DrainTimeout)*time.Second)
	defer cancel()
	left := inside.Drain(ctx, a.m, a.svc)
	require.NoError(t, ctx.Err(), "the drain waited for its deadline of %d s rather than for the streams to end", cfg.DMR.DrainTimeout)
	assert.Equal(t, 0, left, "no stream is left on the draining replica")
	require.Eventually(t, func() bool {
		_, ok := b.m.Running()[p.ID]
		return ok && online(t, c, p.ID)
	}, 30*time.Second, 50*time.Millisecond, "the other replica runs the plugin")
}

func TestAPluginInsideFollowsItsSwitch(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	r := c.replica(t, "a", engine, true)
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9100, Last: 9100})
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 60*time.Second, 50*time.Millisecond)
	_, err := models.UpdatePlugin(c.db, p.ID, p.Name, false, p.Ranges)
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { _, ok := r.m.Running()[p.ID]; return !ok }, 30*time.Second, 50*time.Millisecond)
	_, err = models.UpdatePlugin(c.db, p.ID, p.Name, true, p.Ranges)
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 30*time.Second, 50*time.Millisecond)
	require.NoError(t, models.DeletePlugin(c.db, p.ID))
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { _, ok := r.m.Running()[p.ID]; return !ok }, 30*time.Second, 50*time.Millisecond)
}

func TestANewVersionTakesOverWhenTheCallsEnd(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	r := c.replica(t, "a", engine, true)
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9100, Last: 9100})
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 60*time.Second, 50*time.Millisecond)
	before := r.m.Running()[p.ID]
	call := func(seq uint) {
		t.Helper()
		n := r.router.count()
		require.NoError(t, r.registry.Deliver(p.ID, models.Packet{Src: 3002068, Dst: 9100, StreamID: 5, Seq: seq}))
		require.Eventually(t, func() bool { return r.router.count() > n }, 10*time.Second, 20*time.Millisecond, "the guest echoes burst %d", seq)
	}

	call(0)
	_, err := models.SetPluginModule(c.db, p.ID, models.PluginInfo{Name: "Echo", Version: "2.0.0"}, guest(t, "netprobe"), fake.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	timing := fast()
	for seq := uint(1); time.Duration(seq)*timing.Renew <= 2*timing.Quiet; seq++ {
		fake.Advance(timing.Renew)
		call(seq)
	}
	assert.Equal(t, before, r.m.Running()[p.ID], "the old version stays while a call goes on, past the quiet time")
	fake.Advance(timing.Quiet)
	require.Eventually(t, func() bool {
		now, ok := r.m.Running()[p.ID]
		return ok && now != before
	}, 30*time.Second, 50*time.Millisecond, "the new version takes over after the call")
}

func stops(t *testing.T, c *cluster, id uint) int {
	t.Helper()
	st, _, err := plugins.StateOf(context.Background(), c.kv, id, c.now())
	require.NoError(t, err)
	return len(st.Stops)
}

func TestAPluginThatFailsStartsAgain(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	r := c.replica(t, "a", engine, true)
	p := upload(t, c, "Quit", guest(t, "quit"))
	timing := fast()
	for n := 1; n <= 3; n++ {
		require.Eventually(t, func() bool { return stops(t, c, p.ID) == n }, 60*time.Second, 50*time.Millisecond, "the guest quits")
		assert.Equal(t, n, r.m.Starts(p.ID), "the plugin waits for its backoff before it starts again")
		st, _, err := plugins.StateOf(context.Background(), c.kv, p.ID, c.now())
		require.NoError(t, err)
		backoff := min(timing.MinBackoff<<(n-1), timing.MaxBackoff)
		assert.True(t, st.StoppedAt.Add(backoff).Equal(st.RestartAt), "the backoff after stop %d is %v: %v to %v", n, backoff, st.StoppedAt, st.RestartAt)
		fake.Advance(backoff + timing.Renew)
	}
}

func TestAPluginThatStopsTellsTheOtherReplicasWhy(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	c.replica(t, "a", engine, true)
	p := upload(t, c, "Quit", guest(t, "quit"))
	require.Eventually(t, func() bool { return stops(t, c, p.ID) == 1 }, 60*time.Second, 50*time.Millisecond, "the guest quits")
	st, ok, err := plugins.StateOf(context.Background(), c.kv, p.ID, c.now())
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "It crashed: quitting.", st.Reason)
	assert.True(t, fake.Now().Equal(st.StoppedAt), "it says when the plugin stopped: %v", st.StoppedAt)
	assert.True(t, st.StoppedAt.Add(fast().MinBackoff).Equal(st.RestartAt), "it says when the plugin starts again: %v", st.RestartAt)
}

func TestAnExpiredLeaseLetsAnotherReplicaRunThePlugin(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	c := newClusterOn(t, fake)
	p := upload(t, c, "Echo", guest(t, "echo"), models.PluginRange{First: 9100, Last: 9100})
	timing := fast()
	ok, err := c.kv.SetNX(context.Background(), inside.LeaseKey(p.ID), "gone", timing.Lease)
	require.NoError(t, err)
	require.True(t, ok)
	b := c.replica(t, "b", engine, true)
	for step := timing.Renew; step < timing.Lease; step += timing.Renew {
		fake.Advance(timing.Renew)
		assert.Empty(t, b.m.Running(), "the lease of a replica that went away holds until it expires")
	}
	fake.Advance(2 * timing.Renew)
	require.Eventually(t, func() bool {
		_, ok := b.m.Running()[p.ID]
		return ok && online(t, c, p.ID)
	}, 60*time.Second, 50*time.Millisecond, "the other replica runs the plugin once the lease expired")
}

// pong listens on loopback and answers each connection's "ping" with
// "pong".
func pong(t *testing.T) string {
	t.Helper()
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = lis.Close() })
	go func() {
		for {
			conn, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(conn, buf); err == nil {
					_, _ = conn.Write([]byte("pong"))
				}
			}()
		}
	}()
	return lis.Addr().String()
}

func TestTheAdminAllowsAndRevokesTheHostsOfAPlugin(t *testing.T) {
	t.Parallel()
	c := newCluster(t)
	l := wasm.DefaultLimits()
	l.CPUShare = 100
	l.AllowAddress = func(net.IP, bool) bool { return true }
	r := c.replica(t, "a", engine.Limited(l), true)
	addr := pong(t)
	p, err := models.CreateUploadedPlugin(c.db, models.PluginInfo{Name: "Netprobe", Hosts: []models.PluginHost{{Address: addr}}},
		[]models.PluginRange{{First: 9200, Last: 9200}}, nil, guest(t, "netprobe"), time.Now())
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { return online(t, c, p.ID) }, 60*time.Second, 50*time.Millisecond)

	probe := func() string {
		n := r.router.count()
		pkt := models.Packet{Src: 3002068, Dst: 9200, StreamID: 3}
		copy(pkt.DMRData[:], addr)
		require.NoError(t, r.registry.Deliver(p.ID, pkt))
		require.Eventually(t, func() bool { return r.router.count() > n }, 10*time.Second, 20*time.Millisecond)
		got := r.router.last()
		return strings.TrimRight(string(got.DMRData[:]), "\x00")
	}
	assert.True(t, strings.HasPrefix(probe(), "ERR"), "a host starts out not allowed")
	_, err = models.SetAllowedPluginHosts(c.db, p.ID, []string{addr})
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { return probe() == "pong" }, 10*time.Second, 100*time.Millisecond)
	_, err = models.SetAllowedPluginHosts(c.db, p.ID, nil)
	require.NoError(t, err)
	require.NoError(t, plugins.PublishChanged(c.ps))
	require.Eventually(t, func() bool { return strings.HasPrefix(probe(), "ERR") }, 10*time.Second, 100*time.Millisecond)
}
