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

package wasm_test

import (
	"context"
	"io"
	"log/slog"
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/plugins/wasm"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

var engine = wasm.NewCachedEngine(wasm.DefaultLimits()) //nolint:gochecknoglobals // one compilation cache for the tests

type routed struct {
	pkts chan models.Packet
}

func (r *routed) RoutePacket(_ context.Context, p models.Packet, _ string) {
	r.pkts <- p
}

type rig struct {
	db       *gorm.DB
	ps       pubsub.PubSub
	registry *plugins.Registry
	svc      *plugins.Service
	router   *routed
}

func newRig(t *testing.T) *rig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	registry, err := plugins.NewRegistry(database, ps)
	require.NoError(t, err)
	r := &rig{db: database, ps: ps, registry: registry, router: &routed{pkts: make(chan models.Packet, 64)}}
	r.svc = plugins.NewService(database, ps, registry, r.router, nil)
	t.Cleanup(func() {
		_ = ps.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return r
}

// run starts the module wasm as the uploaded plugin name with ranges, on
// the service of r, until the test ends.
func (r *rig) run(t *testing.T, e *wasm.Engine, name string, wasmBytes []byte, hosts wasm.Hosts, ranges ...models.PluginRange) (*wasm.Instance, models.Plugin) {
	t.Helper()
	p, err := models.CreateUploadedPlugin(r.db, models.PluginInfo{Name: name}, ranges, nil, wasmBytes, time.Now())
	require.NoError(t, err)
	require.NoError(t, r.registry.Reload())
	inst, err := e.Start(context.Background(), name, wasmBytes, hosts)
	require.NoError(t, err)
	t.Cleanup(inst.Stop)
	ctx, cancel := context.WithCancel(context.Background())
	served := make(chan struct{})
	go func() {
		defer close(served)
		_ = r.svc.Connect(inst.Stream(plugins.InProcess(ctx, p.ID))) //nolint:contextcheck // the stream carries ctx
	}()
	t.Cleanup(func() {
		cancel()
		inst.Stop()
		<-served
	})
	require.Eventually(t, func() bool {
		select {
		case <-inst.Done():
			return true
		default:
		}
		got, err := models.FindPlugin(r.db, p.ID)
		return err == nil && got.Online(time.Now())
	}, 10*time.Second, 10*time.Millisecond, "the plugin %s connects", name)
	return inst, p
}

func (r *rig) next(t *testing.T) models.Packet {
	t.Helper()
	select {
	case p := <-r.router.pkts:
		return p
	case <-time.After(10 * time.Second):
		t.Fatal("the plugin sent nothing")
		return models.Packet{}
	}
}

func TestInspectReadsTheHello(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		h, err := engine.Inspect(context.Background(), guestOf(t, compiler, "echo"))
		require.NoError(t, err)
		assert.Equal(t, "Echo", h.GetName())
		assert.Equal(t, "1.0.0", h.GetVersion())
		assert.Equal(t, "N0CALL", h.GetAuthor())
		require.Len(t, h.GetIds(), 1)
		assert.Equal(t, uint32(9100), h.GetIds()[0].GetFirst())
	})
}

func TestInspectRefusesWhatIsNotWasm(t *testing.T) {
	t.Parallel()
	_, err := engine.Inspect(context.Background(), []byte("#!/bin/sh\necho hi\n"))
	require.ErrorIs(t, err, wasm.ErrNotWasm)
}

func TestInspectRefusesAProgramThatSaysNoHello(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		l := wasm.DefaultLimits()
		l.HelloWithin = 500 * time.Millisecond
		started := time.Now()
		_, err := engine.Limited(l).Inspect(context.Background(), guestOf(t, compiler, "silent"))
		require.ErrorIs(t, err, wasm.ErrNoHello)
		assert.Less(t, time.Since(started), time.Minute, "a program asleep for an hour is stopped")
	})
}

func TestAPluginInsideReachesNoFilesAndNoEnvironment(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		h, err := engine.Inspect(context.Background(), guestOf(t, compiler, "files"))
		require.NoError(t, err)
		assert.Equal(t, "Files", h.GetName())
		assert.Empty(t, h.GetDescription(), "the plugin found: %s", h.GetDescription())
	})
}

func TestAPluginInsideAnswersThroughTheService(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		r := newRig(t)
		_, p := r.run(t, engine, "Echo", guestOf(t, compiler, "echo"), nil, models.PluginRange{First: 9100, Last: 9100})
		require.NoError(t, r.registry.Deliver(p.ID, models.Packet{Src: 3191868, Dst: 9100, StreamID: 7, Seq: 1, Slot: true}))
		got := r.next(t)
		assert.Equal(t, uint(9100), got.Src)
		assert.Equal(t, uint(3191868), got.Dst)
		assert.False(t, got.GroupCall)
		assert.Equal(t, uint(7), got.StreamID)
	})
}

func TestStopEndsAPluginThatWaitsForTheHub(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		inst, err := engine.Start(context.Background(), "Echo", guestOf(t, compiler, "echo"), nil)
		require.NoError(t, err)
		assert.Equal(t, "Echo", inst.Hello().GetName())
		started := time.Now()
		inst.Stop()
		select {
		case <-inst.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("the plugin still runs")
		}
		assert.Less(t, time.Since(started), 5*time.Second)
	})
}

func TestAPluginThatSpinsIsStopped(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		l := wasm.DefaultLimits()
		l.CPUWindow = time.Second
		inst, _ := newRig(t).run(t, engine.Limited(l), "Spin", guestOf(t, compiler, "spin"), nil)
		select {
		case <-inst.Done():
		case <-time.After(15 * time.Second):
			t.Fatal("the plugin still spins")
		}
		require.ErrorIs(t, inst.Err(), wasm.ErrCPU)
	})
}

func TestAPluginThatDoesNotReadIsStopped(t *testing.T) {
	t.Parallel()
	l := wasm.DefaultLimits()
	l.AnswerWithin = 500 * time.Millisecond
	inst, err := engine.Limited(l).Start(context.Background(), "Deaf", guest(t, "deaf"), nil)
	require.NoError(t, err)
	t.Cleanup(inst.Stop)
	stream := inst.Stream(context.Background())
	go func() {
		burst := &pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: &pluginv1.Burst{Payload: make([]byte, 1024)}}}
		for stream.Send(burst) == nil {
		}
	}()
	select {
	case <-inst.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("the plugin still runs")
	}
	require.ErrorIs(t, inst.Err(), wasm.ErrNoAnswer)
}

func TestAPluginThatEndsBeforeItsHelloSaysWhy(t *testing.T) {
	t.Parallel()
	_, err := engine.Start(context.Background(), "Early", guest(t, "early"), nil)
	require.ErrorIs(t, err, wasm.ErrNoHello)
	require.ErrorIs(t, err, wasm.ErrEnded)
	var ended *wasm.EndedError
	require.ErrorAs(t, err, &ended)
	assert.Equal(t, "panic: no config", ended.Message())
}

func TestAPluginThatTakesTooMuchMemoryIsStopped(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		l := wasm.DefaultLimits()
		l.MemoryPages = 512
		l.CPUShare = 100
		inst, _ := newRig(t).run(t, engine.Limited(l), "Hog", guestOf(t, compiler, "hog"), nil)
		select {
		case <-inst.Done():
		case <-time.After(time.Minute):
			t.Fatal("the plugin still grows")
		}
		require.ErrorIs(t, inst.Err(), wasm.ErrMemory)
	})
}

// logs is a slog handler that keeps the lines that plugins log.
type logs struct {
	mu    sync.Mutex
	lines []string
}

func (*logs) Enabled(context.Context, slog.Level) bool { return true }

func (l *logs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	r.Attrs(func(a slog.Attr) bool {
		if a.Key == "line" {
			l.lines = append(l.lines, a.Value.String())
		}
		return true
	})
	return nil
}

func (l *logs) WithAttrs([]slog.Attr) slog.Handler { return l }

func (l *logs) WithGroup(string) slog.Handler { return l }

func (l *logs) has(line string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return slices.Contains(l.lines, line)
}

// TestAPluginThatPanicsFailsAndLogsItsPanic runs alone, as it replaces the
// default logger. TinyGo writes a panic on stdout, where the hub reads the
// messages of the plugin, so the hub logs it as it stops the plugin.
func TestAPluginThatPanicsFailsAndLogsItsPanic(t *testing.T) { //nolint:paralleltest // it replaces the default logger
	l := &logs{}
	old := slog.Default()
	slog.SetDefault(slog.New(l))
	t.Cleanup(func() { slog.SetDefault(old) })
	for _, compiler := range compilers { //nolint:paralleltest // it replaces the default logger
		t.Run(compiler, func(t *testing.T) {
			inst, _ := newRig(t).run(t, engine, "Panic", guestOf(t, compiler, "panic"), nil)
			select {
			case <-inst.Done():
			case <-time.After(10 * time.Second):
				t.Fatal("the plugin still runs")
			}
			require.Error(t, inst.Err())
			require.NotErrorIs(t, inst.Err(), wasm.ErrExited)
			require.Eventually(t, func() bool { return l.has("panic: the panic of the test plugin") }, 5*time.Second, 10*time.Millisecond)
		})
	}
}

// startsWith returns a module whose memory starts with pages, and whose
// _start does nothing.
func startsWith(pages byte) []byte {
	return []byte{
		0x00, 0x61, 0x73, 0x6d, 0x01, 0x00, 0x00, 0x00,
		0x01, 0x04, 0x01, 0x60, 0x00, 0x00,
		0x03, 0x02, 0x01, 0x00,
		0x05, 0x03, 0x01, 0x00, pages,
		0x07, 0x13, 0x02, 0x06, 'm', 'e', 'm', 'o', 'r', 'y', 0x02, 0x00, 0x06, '_', 's', 't', 'a', 'r', 't', 0x00, 0x00,
		0x0a, 0x04, 0x01, 0x02, 0x00, 0x0b,
	}
}

func TestAPluginWhoseMemoryStartsPastItsLimitDoesNotStart(t *testing.T) {
	t.Parallel()
	l := wasm.DefaultLimits()
	l.MemoryPages = 16
	_, err := engine.Limited(l).Start(context.Background(), "Big", startsWith(20), nil)
	require.ErrorIs(t, err, wasm.ErrMemory)
	_, err = engine.Limited(l).Start(context.Background(), "Bigger", startsWith(40), nil)
	require.ErrorIs(t, err, wasm.ErrNotWasm)
	_, err = engine.Limited(l).Start(context.Background(), "Small", startsWith(16), nil)
	require.ErrorIs(t, err, wasm.ErrNoHello)
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
			c, err := lis.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = c.Close() }()
				buf := make([]byte, 4)
				if _, err := io.ReadFull(c, buf); err == nil && string(buf) == "ping" {
					_, _ = c.Write([]byte("pong"))
				}
			}()
		}
	}()
	return lis.Addr().String()
}

func probe(t *testing.T, r *rig, p models.Plugin, address string) string {
	t.Helper()
	pkt := models.Packet{Src: 3191868, Dst: 9200, StreamID: 9}
	copy(pkt.DMRData[:], address)
	require.NoError(t, r.registry.Deliver(p.ID, pkt))
	got := r.next(t)
	return strings.TrimRight(string(got.DMRData[:]), "\x00")
}

func loopbackAllowed() wasm.Limits {
	l := wasm.DefaultLimits()
	l.AllowAddress = func(net.IP, bool) bool { return true }
	return l
}

func TestAPluginInsideReachesTheHostsTheAdminAllowed(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		r := newRig(t)
		allowed := pong(t)
		other := pong(t)
		hosts := func(a string) bool { return a == allowed }
		_, p := r.run(t, engine.Limited(loopbackAllowed()), "Netprobe", guestOf(t, compiler, "netprobe"), hosts, models.PluginRange{First: 9200, Last: 9200})
		assert.Equal(t, "pong", probe(t, r, p, allowed))
		refused := probe(t, r, p, other)
		assert.True(t, strings.HasPrefix(refused, "ERR"), "a host the admin did not allow: %q", refused)
	})
}

func TestAPluginInsideNeverReachesLoopbackByDefault(t *testing.T) {
	t.Parallel()
	eachCompiler(t, func(t *testing.T, compiler string) {
		r := newRig(t)
		addr := pong(t)
		_, port, err := net.SplitHostPort(addr)
		require.NoError(t, err)
		byName := net.JoinHostPort("localhost", port)
		hosts := func(a string) bool { return a == addr || a == byName }
		_, p := r.run(t, engine, "Netprobe", guestOf(t, compiler, "netprobe"), hosts, models.PluginRange{First: 9200, Last: 9200})
		for _, a := range []string{addr, byName} {
			got := probe(t, r, p, a)
			assert.True(t, strings.HasPrefix(got, "ERR"), "%s: %q", a, got)
		}
	})
}
