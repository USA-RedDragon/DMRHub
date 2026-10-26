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

// Package inside runs the plugins inside DMRHub: the built-in parrot and
// location server, and the plugins that admins upload. Of the replicas that
// share a KV store, only the one that holds a plugin's lease runs it.
package inside

import (
	"context"
	"encoding/hex"
	"errors"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/builtin"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"gorm.io/gorm"
)

// leasePrefix starts the KV key of the lease of each plugin inside DMRHub.
const leasePrefix = "dmrhub:plugins:inside:"

// Timing is how often the Manager renews its leases, how long a lease
// lasts, how long a plugin must be quiet before a new version takes over and
// the longest wait for that, and the waits before a failed plugin starts
// again.
type Timing struct {
	Renew      time.Duration
	Lease      time.Duration
	Quiet      time.Duration
	MaxWait    time.Duration
	MinBackoff time.Duration
	MaxBackoff time.Duration
}

// DefaultTiming renews each lease every 5 seconds for 15 seconds, lets a
// new version take over after 5 quiet seconds or 5 minutes at most, and
// starts a failed plugin again after a wait that doubles from a second to a
// minute.
func DefaultTiming() Timing {
	return Timing{
		Renew: 5 * time.Second, Lease: 15 * time.Second, Quiet: 5 * time.Second, MaxWait: 5 * time.Minute,
		MinBackoff: time.Second, MaxBackoff: time.Minute,
	}
}

// Deps is what the Manager uses.
type Deps struct {
	DB     *gorm.DB
	PubSub pubsub.PubSub
	KV     kv.KV
	// Instance is the ID of this replica, which holds the leases.
	Instance string
	// Version is DMRHub's version, the version of the built-in plugins.
	Version string
	// Service and Registry serve the plugins on this replica.
	Service  *plugins.Service
	Registry *plugins.Registry
	// Engine runs the plugins; nil runs them within wasm.DefaultLimits.
	Engine *wasm.Engine
	// Builtins are the plugins that come with DMRHub; nil is builtin.All.
	Builtins []builtin.Plugin
	// Timing is zero for DefaultTiming.
	Timing Timing
}

// Manager runs the plugins inside DMRHub whose leases this replica holds.
type Manager struct {
	d        Deps
	builtins map[string]builtin.Plugin

	kick    chan struct{}
	stopped chan struct{}
	cancel  context.CancelFunc
	wg      sync.WaitGroup

	on     atomic.Bool
	mu     sync.Mutex
	runs   map[uint]*run
	starts map[uint]int
	holds  map[uint]bool
}

// run is a plugin that this replica runs, or starts. inst is set before
// ready is closed, and stays nil when the plugin could not start.
type run struct {
	id       uint
	key      string
	inst     *wasm.Instance
	hosts    atomic.Pointer[[]string]
	last     atomic.Int64
	since    time.Time
	ready    chan struct{}
	ended    chan struct{}
	stopping atomic.Bool
	retire   time.Time
	err      error
}

// started returns the instance of r once it started, and nil before or
// when it could not start.
func (r *run) started() *wasm.Instance {
	select {
	case <-r.ready:
		return r.inst
	default:
		return nil
	}
}

// NewManager returns a Manager of d.
func NewManager(d Deps) *Manager {
	if d.Engine == nil {
		d.Engine = wasm.NewEngine(wasm.DefaultLimits())
	}
	if d.Builtins == nil {
		d.Builtins = builtin.All()
	}
	if d.Timing == (Timing{}) {
		d.Timing = DefaultTiming()
	}
	m := &Manager{
		d: d, builtins: map[string]builtin.Plugin{}, kick: make(chan struct{}, 1), stopped: make(chan struct{}),
		runs: map[uint]*run{}, starts: map[uint]int{}, holds: map[uint]bool{},
	}
	for _, b := range d.Builtins {
		m.builtins[b.Key] = b
	}
	return m
}

// Start runs the plugins inside DMRHub when on, and follows the changes of
// the plugins until Stop.
func (m *Manager) Start(ctx context.Context, on bool) error {
	ctx, m.cancel = context.WithCancel(context.WithoutCancel(ctx))
	if err := m.SetOn(ctx, on); err != nil {
		m.cancel()
		return err
	}
	sub := m.d.PubSub.Subscribe(plugins.ChangedTopic)
	m.wg.Go(func() {
		defer func() { _ = sub.Close() }()
		for {
			select {
			case <-ctx.Done():
				return
			case _, ok := <-sub.Channel():
				if !ok {
					return
				}
				m.poke()
			}
		}
	})
	m.wg.Go(func() { m.loop(ctx) })
	return nil
}

// SetOn turns the plugins inside DMRHub on or off on this replica. Turning
// them on adds the built-in plugins that are not there yet.
func (m *Manager) SetOn(ctx context.Context, on bool) error {
	if on {
		m.seed()
	}
	m.on.Store(on)
	if err := m.d.Registry.SetInProcess(on); err != nil {
		return err //nolint:wrapcheck // the registry's error
	}
	m.poke()
	_ = ctx
	return nil
}

func (m *Manager) seed() {
	added := false
	for _, b := range m.d.Builtins {
		if _, err := models.EnsureBuiltinPlugin(m.d.DB, b.Key, b.Name, m.d.Version, b.Ranges, time.Now()); err != nil {
			slog.Warn("Plugins: cannot add a built-in plugin", "plugin", b.Key, "error", err)
			continue
		}
		added = true
	}
	if added {
		if err := plugins.PublishChanged(m.d.PubSub); err != nil {
			slog.Warn("Plugins: cannot announce the built-in plugins", "error", err)
		}
	}
}

func (m *Manager) poke() {
	select {
	case m.kick <- struct{}{}:
	default:
	}
}

// Stop stops the plugins that this replica runs and gives up their
// leases, so that another replica takes them over.
func (m *Manager) Stop(ctx context.Context) {
	if m.cancel == nil {
		return
	}
	m.cancel()
	m.wg.Wait()
	m.mu.Lock()
	runs := m.runs
	m.runs = map[uint]*run{}
	m.mu.Unlock()
	for _, r := range runs {
		r.stopping.Store(true)
		<-r.ready
		if r.inst != nil {
			r.inst.Stop()
		}
		<-r.ended
	}
	released := false
	for id, held := range m.holds {
		released = released || held
		m.release(ctx, id)
	}
	if released {
		if err := plugins.PublishChanged(m.d.PubSub); err != nil {
			slog.Warn("Plugins: cannot announce the plugins that this replica gave up", "error", err)
		}
	}
}

// Running returns the plugins that this replica runs, each with the key of
// its module.
func (m *Manager) Running() map[uint]string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make(map[uint]string, len(m.runs))
	for id, r := range m.runs {
		out[id] = r.key
	}
	return out
}

// Starts returns how many times this replica started the plugin id.
func (m *Manager) Starts(id uint) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.starts[id]
}

func (m *Manager) loop(ctx context.Context) {
	t := time.NewTicker(m.d.Timing.Renew)
	defer t.Stop()
	waits := map[uint]time.Time{}
	backoffs := map[uint]time.Duration{}
	for {
		m.reconcile(ctx, waits, backoffs)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-m.kick:
		}
	}
}

func moduleKey(p *models.Plugin, version string) string {
	if p.Kind == models.PluginBuiltin {
		return "builtin:" + p.Builtin + "@" + version
	}
	return hex.EncodeToString(p.ModuleHash)
}

func allowedOf(p *models.Plugin) []string {
	var out []string
	for _, h := range p.Hosts {
		if h.Allowed {
			out = append(out, h.Address)
		}
	}
	return out
}

func sameHosts(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// reconcile starts the plugins that this replica should run and stops the
// others. waits holds when each failed plugin may start again, and
// backoffs the wait after its next failure.
func (m *Manager) reconcile(ctx context.Context, waits map[uint]time.Time, backoffs map[uint]time.Duration) {
	for _, s := range m.reap(waits, backoffs) {
		if err := plugins.NoteStop(ctx, m.d.KV, s.id, s.at, s.restart, s.reason); err != nil {
			slog.Warn("Plugins: cannot tell the other replicas that a plugin stopped", "plugin", s.id, "error", err)
		}
	}
	want := map[uint]models.Plugin{}
	if m.on.Load() {
		list, err := models.ListPlugins(m.d.DB)
		if err != nil {
			slog.Error("Plugins: cannot list the plugins inside DMRHub", "error", err)
			return
		}
		waiting := models.WaitingPlugins(list)
		for _, p := range list {
			if _, wait := waiting[p.ID]; !wait && p.Kind != models.PluginExternal && p.Enabled {
				want[p.ID] = p
			}
		}
	}
	m.mu.Lock()
	running := make(map[uint]*run, len(m.runs))
	for id, r := range m.runs {
		running[id] = r
	}
	m.mu.Unlock()
	for id, r := range running {
		if _, ok := want[id]; !ok {
			m.stop(r)
		}
	}
	for id := range m.holds {
		if _, ok := want[id]; !ok {
			m.release(ctx, id)
		}
	}
	now := time.Now()
	for id, p := range want {
		if !m.hold(ctx, id) {
			if r := running[id]; r != nil {
				m.stop(r)
			}
			continue
		}
		key := moduleKey(&p, m.d.Version)
		if r := running[id]; r != nil {
			if hosts := allowedOf(&p); !sameHosts(hosts, *r.hosts.Load()) {
				r.hosts.Store(&hosts)
				if inst := r.started(); inst != nil {
					inst.Refresh()
				}
			}
			if r.key != key {
				m.retire(r, now)
			}
			continue
		}
		if now.Before(waits[id]) {
			continue
		}
		m.start(ctx, &p, key)
	}
}

// retire stops r, a plugin whose module changed, once it has been quiet
// for a while, or waited too long.
func (m *Manager) retire(r *run, now time.Time) {
	if r.retire.IsZero() {
		r.retire = now
		slog.Info("Plugins: a new version waits for the calls of the plugin to end", "plugin", r.id)
	}
	quiet := now.Sub(time.Unix(0, r.last.Load())) >= m.d.Timing.Quiet
	if quiet || now.Sub(r.retire) >= m.d.Timing.MaxWait {
		m.stop(r)
	}
}

// stopped is a plugin that stopped by itself.
type stopped struct {
	id          uint
	at, restart time.Time
	reason      string
}

// reap forgets the plugins that ended, sets when each may start again,
// and returns those that stopped by themselves.
func (m *Manager) reap(waits map[uint]time.Time, backoffs map[uint]time.Duration) []stopped {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	var out []stopped
	for id, r := range m.runs {
		select {
		case <-r.ended:
		default:
			continue
		}
		delete(m.runs, id)
		if !r.retire.IsZero() || r.stopping.Load() || (r.inst != nil && errors.Is(r.inst.Err(), wasm.ErrStopped)) {
			delete(waits, id)
			delete(backoffs, id)
			continue
		}
		wait := backoffs[id]
		if wait == 0 || now.Sub(r.since) > m.d.Timing.MaxBackoff {
			wait = m.d.Timing.MinBackoff
		}
		waits[id] = now.Add(wait)
		backoffs[id] = min(wait*2, m.d.Timing.MaxBackoff) //nolint:mnd // the wait doubles
		out = append(out, stopped{id: id, at: now, restart: waits[id], reason: m.reasonOf(r)})
	}
	return out
}

// reasonOf says why the plugin of r stopped by itself.
func (m *Manager) reasonOf(r *run) string {
	if r.inst == nil {
		return reasonOf(r.err, "", m.d.Engine.Limits())
	}
	return reasonOf(r.inst.Err(), r.inst.Said(), m.d.Engine.Limits())
}

// stop stops r in the background. The plugin stays among the running ones
// until it ended, so that it never runs twice on this replica.
func (m *Manager) stop(r *run) {
	if r.stopping.Swap(true) {
		return
	}
	m.wg.Go(func() {
		<-r.ready
		if r.inst != nil {
			r.inst.Stop()
		}
	})
}

func (m *Manager) module(p *models.Plugin) ([]byte, []string, error) {
	if p.Kind == models.PluginBuiltin {
		b, ok := m.builtins[p.Builtin]
		if !ok {
			return nil, nil, errNoBuiltin
		}
		module, err := b.Module()
		return module, b.Args(), err //nolint:wrapcheck // the error names the plugin
	}
	_, module, err := models.PluginModuleOf(m.d.DB, p.ID)
	return module, nil, err //nolint:wrapcheck // the database's error
}

var errNoBuiltin = errors.New("this DMRHub has no such built-in plugin")

// start starts the plugin p in the background, so that a plugin that is
// slow to compile never holds up the leases.
func (m *Manager) start(ctx context.Context, p *models.Plugin, key string) {
	r := &run{id: p.ID, key: key, since: time.Now(), ready: make(chan struct{}), ended: make(chan struct{})}
	hosts := allowedOf(p)
	r.hosts.Store(&hosts)
	r.last.Store(time.Now().UnixNano())
	m.mu.Lock()
	m.runs[p.ID] = r
	m.starts[p.ID]++
	m.mu.Unlock()
	plugin := *p
	m.wg.Go(func() {
		defer m.poke()
		defer close(r.ended)
		inst, err := m.launch(ctx, &plugin, r)
		r.inst, r.err = inst, err
		close(r.ready)
		if inst == nil {
			return
		}
		if r.stopping.Load() {
			inst.Stop()
			return
		}
		slog.Info("Plugins: running a plugin inside DMRHub", "plugin", plugin.Name, "version", plugin.Version)
		err = m.d.Service.Connect(&tracked{PluginService_ConnectServer: inst.Stream(plugins.InProcess(ctx, plugin.ID)), r: r}) //nolint:contextcheck // the stream carries ctx
		if err != nil {
			slog.Info("Plugins: the stream of a plugin inside DMRHub ended", "plugin", plugin.Name, "reason", err)
		}
		inst.Stop()
	})
}

// launch compiles and starts the module of p, and returns nil when it
// cannot.
func (m *Manager) launch(ctx context.Context, p *models.Plugin, r *run) (*wasm.Instance, error) {
	module, args, err := m.module(p)
	if err != nil {
		slog.Error("Plugins: cannot read the module of a plugin", "plugin", p.Name, "error", err)
		return nil, err
	}
	inst, err := m.d.Engine.Start(ctx, p.Name, module, func(address string) bool {
		for _, h := range *r.hosts.Load() {
			if strings.EqualFold(h, address) {
				return true
			}
		}
		return false
	}, args...)
	if err != nil {
		slog.Error("Plugins: cannot start a plugin inside DMRHub", "plugin", p.Name, "error", err)
		return nil, err //nolint:wrapcheck // the error says why the plugin stopped
	}
	return inst, nil
}

// tracked notes when a plugin last sent or got a message.
type tracked struct {
	pluginv1.PluginService_ConnectServer
	r *run
}

// Send sends m to the plugin.
func (t *tracked) Send(m *pluginv1.ConnectResponse) error {
	if m.GetBurst() != nil {
		t.r.last.Store(time.Now().UnixNano())
	}
	return t.PluginService_ConnectServer.Send(m) //nolint:wrapcheck // the stream's error
}

// Recv returns the next message of the plugin.
func (t *tracked) Recv() (*pluginv1.ConnectRequest, error) {
	m, err := t.PluginService_ConnectServer.Recv()
	if m.GetBurst() != nil {
		t.r.last.Store(time.Now().UnixNano())
	}
	return m, err //nolint:wrapcheck // the stream's error
}

func leaseKey(id uint) string {
	return leasePrefix + strconv.FormatUint(uint64(id), 10)
}

// hold takes or renews the lease of the plugin id, and reports whether this
// replica holds it.
func (m *Manager) hold(ctx context.Context, id uint) bool {
	key := leaseKey(id)
	if v, err := m.d.KV.Get(ctx, key); err == nil && string(v) == m.d.Instance {
		if err := m.d.KV.Expire(ctx, key, m.d.Timing.Lease); err != nil {
			slog.Error("Plugins: cannot keep the lease of a plugin", "plugin", id, "error", err)
			m.holds[id] = false
			return false
		}
		m.holds[id] = true
		return true
	}
	ok, err := m.d.KV.SetNX(ctx, key, m.d.Instance, m.d.Timing.Lease)
	if err != nil {
		slog.Error("Plugins: cannot take the lease of a plugin", "plugin", id, "error", err)
	}
	m.holds[id] = ok && err == nil
	return m.holds[id]
}

func (m *Manager) release(ctx context.Context, id uint) {
	if m.holds[id] {
		key := leaseKey(id)
		if v, err := m.d.KV.Get(ctx, key); err == nil && string(v) == m.d.Instance {
			_ = m.d.KV.Delete(ctx, key)
		}
	}
	delete(m.holds, id)
}

// Drainer moves the plugins that run as their own programs to the other
// replicas, as plugins.Service.Drain does.
type Drainer interface {
	Drain(ctx context.Context) int
}

// Drain moves the plugins of this replica to the other replicas, until ctx
// ends, and returns how many streams are left. The plugins inside DMRHub
// cannot connect again by themselves, so m stops them first and gives up
// their leases, and another replica starts them. m may be nil.
func Drain(ctx context.Context, m *Manager, d Drainer) int {
	if m != nil {
		m.Stop(ctx)
	}
	return d.Drain(ctx)
}
