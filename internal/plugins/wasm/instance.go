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

package wasm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/sys"
	"google.golang.org/protobuf/proto"
)

const (
	// frameQueue is how many messages of a plugin wait for the hub before
	// the plugin waits.
	frameQueue = 256
	// stopWait is how long Stop waits for a plugin to end.
	stopWait = 5 * time.Second
	// pageSize is the size of a page of WebAssembly memory.
	pageSize = 64 << 10
	// oomMargin is how close to its limit a plugin's memory is when the
	// plugin ran out of it: the Go runtime grows its heap 4 MiB at a time.
	oomMargin = 4 << 20
)

// Hosts says whether an admin allowed a plugin to reach address, as
// host:port.
type Hosts func(address string) bool

// Instance is a running plugin inside DMRHub.
type Instance struct {
	name   string
	limits Limits
	ctx    context.Context //nolint:containedctx // the life of the plugin
	cancel context.CancelCauseFunc
	engine *Engine
	code   *compiled
	meter  *meter
	net    *netHost

	stdinR, stdin *os.File
	wmu           sync.Mutex

	frames  chan *pluginv1.ConnectRequest
	hello   *pluginv1.Hello
	helloed chan struct{}

	done     chan struct{}
	err      error
	stopOnce sync.Once
}

// Start runs the module wasm as the plugin name, which reaches the hosts
// that hosts allows, and returns once the plugin said hello. args follow
// "plugin" in the arguments of the plugin. Connect the plugin to the hub
// with the plugin service and Stream.
func (e *Engine) Start(ctx context.Context, name string, wasm []byte, hosts Hosts, args ...string) (*Instance, error) {
	if !bytes.HasPrefix(wasm, wasmMagic) {
		return nil, ErrNotWasm
	}
	rt, code, err := e.acquire(ctx, wasm)
	if err != nil {
		return nil, err
	}
	l := e.limits
	runCtx, cancel := context.WithCancelCause(context.WithoutCancel(ctx))
	i := &Instance{
		name: name, limits: l, cancel: cancel, engine: e, code: code, meter: newMeter(time.Now()),
		frames: make(chan *pluginv1.ConnectRequest, frameQueue), helloed: make(chan struct{}), done: make(chan struct{}),
	}
	runCtx = context.WithValue(runCtx, meterKey{}, i.meter)
	i.ctx = runCtx
	i.net = newNetHost(i, hosts)
	mod, err := i.instantiate(runCtx, rt, args)
	if err != nil {
		cancel(err)
		e.release(ctx, code)
		return nil, err
	}
	go i.run(runCtx, mod)
	go i.watch()
	if err := i.awaitHello(); err != nil {
		i.Stop()
		return nil, err
	}
	return i, nil
}

func (i *Instance) instantiate(ctx context.Context, rt wazero.Runtime, args []string) (api.Module, error) {
	var err error
	i.stdinR, i.stdin, err = os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("could not make the stdin of the plugin: %w", err)
	}
	cfg := wazero.NewModuleConfig().WithName("").WithArgs(append([]string{"plugin"}, args...)...).
		WithStdin(i.stdinR).WithStdout(&frameWriter{i: i}).WithStderr(newLogWriter(i.name, i.limits.LogLines)).
		WithSysWalltime().WithSysNanotime().WithNanosleep(i.sleep).WithRandSource(rand.Reader).WithStartFunctions()
	mod, err := rt.InstantiateModule(experimental.WithMemoryAllocator(ctx, memoryAllocator), i.code.module, cfg)
	if err != nil {
		_ = i.stdinR.Close()
		_ = i.stdin.Close()
		return nil, fmt.Errorf("%w: %w", ErrNotWasm, err)
	}
	return mod, nil
}

func (i *Instance) sleep(ns int64) {
	t := time.NewTimer(time.Duration(ns))
	defer t.Stop()
	select {
	case <-t.C:
	case <-i.ctx.Done():
	}
}

func (i *Instance) run(ctx context.Context, mod api.Module) {
	i.meter.pin(pinThread())
	_, err := mod.ExportedFunction("_start").Call(ctx)
	memory := uint64(0)
	if m := mod.Memory(); m != nil {
		memory = uint64(m.Size())
	}
	i.finish(ctx, mod, err, memory)
}

func (i *Instance) finish(ctx context.Context, mod api.Module, err error, memory uint64) {
	var exit *sys.ExitError
	limit := uint64(i.limits.MemoryPages) * pageSize
	cause := context.Cause(ctx)
	switch {
	case cause != nil:
		i.err = cause
	case memory+oomMargin > limit:
		i.err = ErrMemory
	case err == nil, errors.As(err, &exit) && exit.ExitCode() == 0:
		i.err = ErrExited
	default:
		i.err = fmt.Errorf("the plugin failed: %w", err)
	}
	i.cancel(i.err)
	close(i.frames)
	_ = i.stdin.Close()
	_ = i.stdinR.Close()
	i.net.closeAll()
	_ = mod.Close(context.WithoutCancel(ctx))
	i.engine.release(ctx, i.code)
	if !errors.Is(i.err, ErrStopped) {
		slog.Warn("Plugins: a plugin inside DMRHub stopped", "plugin", i.name, "reason", i.err)
	}
	close(i.done)
}

// watch stops the plugin when it uses more CPU than its limit.
func (i *Instance) watch() {
	every := max(i.limits.CPUWindow/10, 50*time.Millisecond) //nolint:mnd // a tenth of the window
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-i.done:
			return
		case now := <-t.C:
			select {
			case <-i.helloed:
			default:
				continue
			}
			if i.meter.over(now, i.limits.CPUWindow, i.limits.CPUShare) {
				i.kill(ErrCPU)
				return
			}
		}
	}
}

func (i *Instance) kill(cause error) {
	i.cancel(cause)
	_ = i.stdin.Close()
}

func (i *Instance) awaitHello() error {
	t := time.NewTimer(i.limits.HelloWithin)
	defer t.Stop()
	select {
	case <-i.helloed:
		return nil
	case <-t.C:
		i.kill(ErrNoHello)
		return ErrNoHello
	case <-i.done:
		return fmt.Errorf("%w: %w", ErrNoHello, i.err)
	}
}

// deliver takes a message that the plugin wrote on its stdout.
func (i *Instance) deliver(m *pluginv1.ConnectRequest) {
	if i.hello == nil {
		h := m.GetHello()
		if h == nil {
			i.kill(ErrNoHello)
			return
		}
		i.hello = h
		i.meter.start(time.Now())
		close(i.helloed)
		return
	}
	if n := m.GetNet(); n != nil {
		i.net.handle(n)
		return
	}
	select {
	case i.frames <- m:
	case <-i.ctx.Done():
	}
}

// write sends m to the plugin on its stdin.
func (i *Instance) write(m *pluginv1.ConnectResponse) error {
	i.wmu.Lock()
	defer i.wmu.Unlock()
	if err := plugin.WriteFrame(i.stdin, m); err != nil {
		return fmt.Errorf("could not reach the plugin: %w", err)
	}
	return nil
}

// Hello returns the hello of the plugin.
func (i *Instance) Hello() *pluginv1.Hello {
	return i.hello
}

// Done is closed when the plugin ended.
func (i *Instance) Done() <-chan struct{} {
	return i.done
}

// Err returns why the plugin ended, once Done is closed.
func (i *Instance) Err() error {
	select {
	case <-i.done:
		return i.err
	default:
		return nil
	}
}

// Refresh closes the connections to the hosts that the plugin may no
// longer reach.
func (i *Instance) Refresh() {
	i.net.refresh()
}

// Stop ends the plugin and waits a little for it to end.
func (i *Instance) Stop() {
	i.stopOnce.Do(func() { i.kill(ErrStopped) })
	t := time.NewTimer(stopWait)
	defer t.Stop()
	select {
	case <-i.done:
	case <-t.C:
		slog.Error("Plugins: a plugin inside DMRHub does not stop", "plugin", i.name)
	}
}

// frameWriter is the stdout of a plugin, which holds the messages of the
// plugin protocol.
type frameWriter struct {
	i   *Instance
	buf []byte
}

func (w *frameWriter) Write(p []byte) (int, error) {
	w.buf = append(w.buf, p...)
	for len(w.buf) >= 4 {
		n := binary.BigEndian.Uint32(w.buf)
		if n > plugin.MaxPluginMessage {
			w.i.kill(ErrProtocol)
			return 0, ErrProtocol
		}
		if uint64(len(w.buf)) < 4+uint64(n) {
			break
		}
		m := &pluginv1.ConnectRequest{}
		if err := proto.Unmarshal(w.buf[4:4+n], m); err != nil {
			w.i.kill(ErrProtocol)
			return 0, ErrProtocol
		}
		w.buf = w.buf[4+n:]
		w.i.deliver(m)
	}
	if len(w.buf) == 0 {
		w.buf = nil
	}
	return len(p), nil
}
