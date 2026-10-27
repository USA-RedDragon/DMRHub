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

// Package manager runs the MMDVM, IPSC, OpenBridge and modem servers of DMRHub
// and puts a change of their settings, and of the AMBE server of the hub,
// into effect without a restart of DMRHub.
package manager

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/ipsc"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/mmdvm"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/modem"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/openbridge"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/liveconfig"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"gorm.io/gorm"
)

// Kind names one of the DMR servers.
type Kind string

const (
	MMDVM      Kind = "mmdvm"
	IPSC       Kind = "ipsc"
	OpenBridge Kind = "openbridge"
	Modem      Kind = "modem"
)

func kinds() []Kind {
	return []Kind{MMDVM, IPSC, OpenBridge, Modem}
}

// Deps is what the servers share with the rest of DMRHub.
type Deps struct {
	Hub     *hub.Hub
	DB      *gorm.DB
	PubSub  pubsub.PubSub
	KV      kv.KV
	Version string
	Commit  string
	// ModemOptions, when set, replaces modem.DefaultOptions, as tests do.
	ModemOptions *modem.Options
}

// Server is a running DMR server.
type Server interface {
	servers.DMRServer
	Addr() net.Addr
}

// ApplyError is a server that could not take its new settings.
type ApplyError struct {
	Server Kind
	// Running is whether the server still runs with its previous settings.
	Running bool
	Err     error
}

func (e ApplyError) Error() string {
	if e.Running {
		return fmt.Sprintf("%s server keeps its previous settings: %v", e.Server, e.Err)
	}
	return fmt.Sprintf("%s server is not running: %v", e.Server, e.Err)
}

func (e ApplyError) Unwrap() error {
	return e.Err
}

// settings is what a server is made with. Any change in it restarts the
// server.
type settings struct {
	enabled bool
	bind    string
	port    int
	id      uint32
	wt      wtSettings
	holds   [2]time.Duration
	modem   config.Modem
}

// wtSettings is config.MMDVMWebTransport in a form that compares, with the
// allowed origins joined by newlines.
type wtSettings struct {
	enabled  bool
	port     int
	certFile string
	keyFile  string
	origins  string
}

func settingsOf(k Kind, c *config.Config) settings {
	switch k {
	case MMDVM:
		s := settings{enabled: true, bind: c.DMR.MMDVM.Bind, port: c.DMR.MMDVM.Port}
		if wt := c.DMR.MMDVM.WebTransport; wt.Enabled {
			s.wt = wtSettings{enabled: true, port: wt.Port, certFile: wt.CertFile, keyFile: wt.KeyFile, origins: strings.Join(wt.AllowedOrigins, "\n")}
		}
		return s
	case IPSC:
		return settings{enabled: c.DMR.IPSC.Enabled, bind: c.DMR.IPSC.Bind, port: c.DMR.IPSC.Port, id: c.DMR.IPSC.NetworkID, holds: [2]time.Duration{c.DMR.IPSC.CallHangHold, c.DMR.IPSC.DataHangHold}}
	case OpenBridge:
		return settings{enabled: c.DMR.OpenBridge.Enabled, bind: c.DMR.OpenBridge.Bind, port: c.DMR.OpenBridge.Port, id: c.DMR.OpenBridge.ServerID}
	case Modem:
		return settings{enabled: c.DMR.Modem.Enabled, modem: c.DMR.Modem}
	}
	return settings{}
}

// copySettings sets the settings of server k in dst to those in src.
func copySettings(k Kind, dst, src *config.Config) {
	switch k {
	case MMDVM:
		dst.DMR.MMDVM = src.DMR.MMDVM
	case IPSC:
		dst.DMR.IPSC = src.DMR.IPSC
	case OpenBridge:
		dst.DMR.OpenBridge = src.DMR.OpenBridge
	case Modem:
		dst.DMR.Modem = src.DMR.Modem
	}
}

func disable(k Kind, c *config.Config) {
	switch k {
	case MMDVM:
	case IPSC:
		c.DMR.IPSC.Enabled = false
	case OpenBridge:
		c.DMR.OpenBridge.Enabled = false
	case Modem:
		c.DMR.Modem.Enabled = false
	}
}

type running struct {
	settings settings
	cfg      *config.Config
	server   Server
}

// Manager runs the DMR servers.
type Manager struct {
	deps Deps

	mu      sync.Mutex
	ctx     context.Context //nolint:containedctx
	servers map[Kind]*running
	stopped bool
}

// New returns a Manager that runs no server yet.
func New(deps Deps) *Manager {
	return &Manager{deps: deps, servers: map[Kind]*running{}}
}

// Start starts the servers that cfg turns on. The servers run until Stop,
// whatever happens to ctx.
func (m *Manager) Start(ctx context.Context, cfg *config.Config) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.ctx = context.WithoutCancel(ctx)
	if m.deps.Hub != nil {
		m.deps.Hub.SetAMBEServers(ambeServers(cfg)) //nolint:contextcheck // the probes of the servers outlive Start; Hub.Stop ends them
	}
	for _, k := range kinds() {
		if !settingsOf(k, cfg).enabled {
			continue
		}
		r, err := m.launch(k, cfg) //nolint:contextcheck // a server outlives Start; Stop ends it
		if err != nil {
			m.stopAll(ctx)
			return fmt.Errorf("failed to start the %s server: %w", k, err)
		}
		m.servers[k] = r
	}
	return nil
}

// Stop stops every server. ctx tells the servers whether this is a
// graceful handoff to another replica. Apply does nothing after Stop.
func (m *Manager) Stop(ctx context.Context) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.stopped = true
	m.stopAll(ctx)
}

// Registry is the record of the replicas of DMRHub that Drain needs.
type Registry interface {
	OtherInstancesExist(ctx context.Context) bool
	MarkDraining(ctx context.Context)
}

// Drain keeps the servers running, as in a replica that stops in a rolling
// restart, while the other replicas take its sessions over: a load
// balancer moves each repeater at its next packet, and the replica that
// gets it resumes the session, since reg marks this one as draining. It
// returns once another replica holds every repeater of this one, or after
// timeout, with the number of repeaters that are left. A lone instance
// does not drain.
func (m *Manager) Drain(ctx context.Context, reg Registry, timeout time.Duration) int {
	if m.deps.Hub == nil || reg == nil || !reg.OtherInstancesExist(ctx) {
		return 0
	}
	reg.MarkDraining(ctx)
	if s, ok := m.Server(MMDVM).(*mmdvm.Server); ok {
		s.DrainWebTransport(ctx)
	}
	if timeout <= 0 {
		return 0
	}
	slog.Info("Draining: other replicas take over the sessions of this one", "timeout", timeout)
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	left := m.deps.Hub.WaitMoved(ctx)
	slog.Info("Drain finished", "repeatersLeft", left)
	return left
}

// Addr returns the address of server k, or nil when it does not run.
func (m *Manager) Addr(k Kind) net.Addr {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.servers[k]; ok {
		return r.server.Addr()
	}
	return nil
}

// Server returns server k, or nil when it does not run.
func (m *Manager) Server(k Kind) Server {
	m.mu.Lock()
	defer m.mu.Unlock()
	if r, ok := m.servers[k]; ok {
		return r.server
	}
	return nil
}

// Apply puts the server settings of next into effect. It starts the
// servers that next turns on, stops those it turns off and restarts those
// whose settings it changes, and leaves the others alone. A server that
// cannot take its new settings keeps running with its old ones when it
// can; Apply then sets the settings of that server in next back to those
// it runs with, and returns why.
func (m *Manager) Apply(next *config.Config) []ApplyError {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped || m.ctx == nil {
		return nil
	}
	if m.deps.Hub != nil {
		m.deps.Hub.SetAMBEServers(ambeServers(next)) //nolint:contextcheck // the probes of the servers outlive Apply; Hub.Stop ends them
	}
	var errs []ApplyError
	for _, k := range kinds() {
		want := settingsOf(k, next)
		cur := m.servers[k]
		switch {
		case cur == nil && !want.enabled:
			continue
		case cur != nil && cur.settings == want:
			continue
		case !want.enabled:
			slog.Info("Stopping a DMR server that the settings turn off", "server", k)
			m.stop(m.ctx, k)
			continue
		}
		if e := m.restart(k, next); e != nil {
			slog.Error("DMR server could not take its new settings", "server", k, "running", e.Running, "error", e.Err)
			if cur != nil {
				copySettings(k, next, cur.cfg)
			} else {
				disable(k, next)
			}
			errs = append(errs, *e)
		}
	}
	return errs
}

// holdsAPortOf reports whether a server with the settings cur holds a port
// that a server with the settings next binds.
func holdsAPortOf(cur, next settings) bool {
	return cur.port == next.port || (cur.wt.enabled && next.wt.enabled && cur.wt.port == next.wt.port)
}

// restart replaces server k, if any, with one made with next. It binds
// the new socket while the old server still runs, so that an address in
// use leaves the old server alone. Only when the new address overlaps the
// one that the old server holds does it stop the old server first, and it
// starts the old server again when the new one still cannot start.
func (m *Manager) restart(k Kind, next *config.Config) *ApplyError {
	cur := m.servers[k]
	r, err := m.build(k, next)
	switch {
	case err != nil && cur != nil && errors.Is(err, syscall.EADDRINUSE) && holdsAPortOf(cur.settings, settingsOf(k, next)):
		m.stop(m.ctx, k)
		r, err = m.build(k, next)
		if err != nil {
			return m.restore(k, cur, err)
		}
	case err != nil:
		return &ApplyError{Server: k, Running: cur != nil, Err: err}
	case cur != nil:
		slog.Info("Restarting a DMR server with new settings", "server", k)
		m.stop(m.ctx, k)
	}
	if err := r.server.Start(m.ctx); err != nil {
		_ = r.server.Stop(m.ctx)
		if cur == nil {
			return &ApplyError{Server: k, Err: err}
		}
		return m.restore(k, cur, err)
	}
	m.servers[k] = r
	slog.Info("DMR server running", "server", k, "address", r.server.Addr().String())
	return nil
}

// restore starts server k again with the settings of old, after the stop
// of old for a new server that failed with err.
func (m *Manager) restore(k Kind, old *running, err error) *ApplyError {
	r, rerr := m.launch(k, old.cfg)
	if rerr != nil {
		slog.Error("DMR server could not start again with its previous settings", "server", k, "error", rerr)
		return &ApplyError{Server: k, Err: err}
	}
	m.servers[k] = r
	return &ApplyError{Server: k, Running: true, Err: err}
}

func (m *Manager) launch(k Kind, cfg *config.Config) (*running, error) {
	r, err := m.build(k, cfg)
	if err != nil {
		return nil, err
	}
	if err := r.server.Start(m.ctx); err != nil {
		_ = r.server.Stop(m.ctx)
		return nil, fmt.Errorf("failed to start: %w", err)
	}
	return r, nil
}

// build makes server k with the settings of cfg and binds its socket.
func (m *Manager) build(k Kind, cfg *config.Config) (*running, error) {
	own := *cfg
	r := &running{settings: settingsOf(k, &own), cfg: &own}
	switch k {
	case MMDVM:
		s, err := mmdvm.MakeServer(&own, m.deps.Hub, m.deps.DB, m.deps.PubSub, m.deps.KV, m.deps.Version, m.deps.Commit)
		if err != nil {
			return nil, fmt.Errorf("failed to make the MMDVM server: %w", err)
		}
		r.server = &s
	case IPSC:
		s := ipsc.NewIPSCServer(&own, m.deps.Hub, m.deps.DB)
		if err := s.Listen(); err != nil {
			return nil, fmt.Errorf("failed to make the IPSC server: %w", err)
		}
		r.server = s
	case OpenBridge:
		s, err := openbridge.MakeServer(&own, m.deps.Hub, m.deps.DB, m.deps.PubSub, m.deps.KV)
		if err != nil {
			return nil, fmt.Errorf("failed to make the OpenBridge server: %w", err)
		}
		r.server = &s
	case Modem:
		opts := modem.DefaultOptions()
		if m.deps.ModemOptions != nil {
			opts = *m.deps.ModemOptions
		}
		r.server = modem.NewServerWithOptions(&own, m.deps.Hub, m.deps.DB, m.deps.Version, opts)
	}
	return r, nil
}

func (m *Manager) stop(ctx context.Context, k Kind) {
	r, ok := m.servers[k]
	if !ok {
		return
	}
	delete(m.servers, k)
	if err := r.server.Stop(ctx); err != nil {
		slog.Error("Failed to stop server", "server", k, "error", err)
	}
}

func ambeServers(c *config.Config) []transcode.ServerConfig {
	out := make([]transcode.ServerConfig, 0, len(c.DMR.Transcoding.AMBEServers))
	for _, s := range c.DMR.Transcoding.AMBEServers {
		out = append(out, transcode.ServerConfig{Addr: s.Address, Calls: s.Calls})
	}
	return out
}

func (m *Manager) stopAll(ctx context.Context) {
	for _, k := range kinds() {
		m.stop(ctx, k)
	}
}

// LiveErrors returns errs as the reports of liveconfig carry them.
func LiveErrors(errs []ApplyError) []liveconfig.ApplyError {
	out := make([]liveconfig.ApplyError, 0, len(errs))
	for _, e := range errs {
		msg := e.Error()
		if e.Err != nil {
			msg = e.Err.Error()
		}
		out = append(out, liveconfig.ApplyError{Server: string(e.Server), Running: e.Running, Error: msg})
	}
	return out
}
