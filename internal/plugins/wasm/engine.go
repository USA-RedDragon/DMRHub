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
	"crypto/sha256"
	"errors"
	"fmt"
	"runtime/debug"
	"sync"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/experimental"
	"github.com/tetratelabs/wazero/imports/wasi_snapshot_preview1"
)

var (
	// ErrNotWasm is returned for a file that is not a WebAssembly module.
	ErrNotWasm = errors.New("not a WebAssembly module")
	// ErrNotPlugin is returned for a WebAssembly module that is not a
	// program for wasip1, as a plugin is.
	ErrNotPlugin = errors.New("a WebAssembly module, but not a program for wasip1")
	// ErrNoHello is returned for a module that does not start with a hello
	// in time, as a plugin does.
	ErrNoHello = errors.New("the module did not say hello as a plugin")
	// ErrEnded is returned, with ErrNoHello, for a plugin that ended before
	// its hello. Its EndedError says why.
	ErrEnded = errors.New("the plugin stopped before its hello")
	// ErrNoAnswer ends a plugin that did not read what the hub sent it for
	// the AnswerWithin of its limits.
	ErrNoAnswer = errors.New("the plugin did not answer the hub")
	// ErrCPU ends a plugin that used more CPU than its limit.
	ErrCPU = errors.New("the plugin used too much CPU")
	// ErrMemory ends a plugin that used all the memory of its limit.
	ErrMemory = errors.New("the plugin used too much memory")
	// ErrProtocol ends a plugin that sent something other than the messages
	// of the plugin protocol.
	ErrProtocol = errors.New("the plugin broke the plugin protocol")
	// ErrStopped ends a plugin that the hub stopped.
	ErrStopped = errors.New("the hub stopped the plugin")
	// ErrExited is returned for a plugin that ended by itself.
	ErrExited = errors.New("the plugin ended")
)

// wasmMagic starts every WebAssembly module.
var wasmMagic = []byte("\x00asm") //nolint:gochecknoglobals // a constant

// keepCompiled is how long an Engine keeps a module that no plugin runs,
// a little longer than the longest wait before a plugin starts again.
const keepCompiled = 2 * time.Minute

// Engine runs the plugins inside DMRHub, all in one runtime. It compiles
// each module once: the plugins that run the same module share its code,
// and a plugin that starts again does not compile it again. It frees the
// code of a module when no plugin ran it for a while, such as an old
// version, or a file that an admin only checked.
type Engine struct {
	cache  wazero.CompilationCache
	limits Limits
	keep   time.Duration

	compiling sync.Mutex
	mu        sync.Mutex
	rt        wazero.Runtime
	modules   map[[sha256.Size]byte]*compiled
}

// compiled is a module that the plugins of an Engine run.
type compiled struct {
	sum    [sha256.Size]byte
	module wazero.CompiledModule
	users  int
	free   *time.Timer
}

// NewEngine returns an Engine whose plugins run within limits.
func NewEngine(limits Limits) *Engine {
	return &Engine{limits: limits, keep: keepCompiled, modules: map[[sha256.Size]byte]*compiled{}}
}

// NewCachedEngine returns an Engine whose plugins run within limits, and
// that keeps every module it compiled until Close, also for the Engines
// that Limited returns, so that the same module starts again fast. The
// tests use it.
func NewCachedEngine(limits Limits) *Engine {
	e := NewEngine(limits)
	e.cache, e.keep = wazero.NewCompilationCache(), -1
	return e
}

// Limits returns the limits of the plugins of e.
func (e *Engine) Limits() Limits {
	return e.limits
}

// Limited returns an Engine with the cache of e, if any, whose plugins run
// within limits.
func (e *Engine) Limited(limits Limits) *Engine {
	l := NewEngine(limits)
	l.cache, l.keep = e.cache, e.keep
	return l
}

// Close stops the plugins of e and frees its compiled modules.
func (e *Engine) Close(ctx context.Context) error {
	e.mu.Lock()
	for sum, c := range e.modules {
		if c.free != nil {
			c.free.Stop()
		}
		_ = c.module.Close(ctx)
		delete(e.modules, sum)
	}
	rt := e.rt
	e.rt = nil
	e.mu.Unlock()
	var err error
	if rt != nil {
		err = rt.Close(ctx)
	}
	if e.cache != nil {
		err = errors.Join(err, e.cache.Close(ctx))
	}
	if err != nil {
		return fmt.Errorf("could not free the compiled plugins: %w", err)
	}
	return nil
}

// runtime returns the runtime of e, which outlives ctx. Call it with mu
// held.
func (e *Engine) runtime(ctx context.Context) (wazero.Runtime, error) {
	if e.rt != nil {
		return e.rt, nil
	}
	rc := wazero.NewRuntimeConfig()
	if interpret {
		rc = wazero.NewRuntimeConfigInterpreter()
	}
	rc = rc.WithMemoryLimitPages(e.limits.MemoryPages).WithCloseOnContextDone(true)
	if e.cache != nil {
		rc = rc.WithCompilationCache(e.cache)
	}
	ctx = context.WithoutCancel(ctx)
	rt := wazero.NewRuntimeWithConfig(ctx, rc)
	if _, err := wasi_snapshot_preview1.NewBuilder(rt).Instantiate(experimental.WithFunctionListenerFactory(ctx, pollListener{})); err != nil {
		_ = rt.Close(ctx)
		return nil, fmt.Errorf("could not give the plugins WASI: %w", err)
	}
	e.rt = rt
	return rt, nil
}

// acquire returns the compiled module wasm, which it compiles only when no
// plugin of e runs it or ran it a short time ago. Give it back to release.
func (e *Engine) acquire(ctx context.Context, wasm []byte) (wazero.Runtime, *compiled, error) {
	sum := sha256.Sum256(wasm)
	if rt, c := e.reuse(sum); c != nil {
		return rt, c, nil
	}
	e.compiling.Lock()
	defer e.compiling.Unlock()
	if rt, c := e.reuse(sum); c != nil {
		return rt, c, nil
	}
	e.mu.Lock()
	rt, err := e.runtime(ctx)
	e.mu.Unlock()
	if err != nil {
		return nil, nil, err
	}
	module, err := rt.CompileModule(ctx, packData(wasm))
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %w", ErrNotWasm, err)
	}
	if _, ok := module.ExportedFunctions()["_start"]; !ok {
		_ = module.Close(ctx)
		return nil, nil, fmt.Errorf("%w: it has no _start", ErrNotPlugin)
	}
	debug.FreeOSMemory()
	c := &compiled{sum: sum, module: module, users: 1}
	e.mu.Lock()
	e.modules[sum] = c
	e.mu.Unlock()
	return rt, c, nil
}

func (e *Engine) reuse(sum [sha256.Size]byte) (wazero.Runtime, *compiled) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c := e.modules[sum]
	if c == nil {
		return nil, nil
	}
	c.users++
	if c.free != nil {
		c.free.Stop()
		c.free = nil
	}
	return e.rt, c
}

// release gives back a module from acquire, and frees it when no plugin
// ran it for keep.
func (e *Engine) release(ctx context.Context, c *compiled) {
	e.mu.Lock()
	defer e.mu.Unlock()
	c.users--
	if c.users > 0 || e.keep < 0 || e.modules[c.sum] != c {
		return
	}
	ctx = context.WithoutCancel(ctx)
	c.free = time.AfterFunc(e.keep, func() {
		e.mu.Lock()
		defer e.mu.Unlock()
		if c.users == 0 && e.modules[c.sum] == c {
			delete(e.modules, c.sum)
			_ = c.module.Close(ctx)
		}
	})
}

// Inspect runs the module wasm only until it says hello, and returns its
// hello: who the plugin is, the IDs it asks for and the hosts it asks to
// reach.
func (e *Engine) Inspect(ctx context.Context, wasm []byte) (*pluginv1.Hello, error) {
	if !bytes.HasPrefix(wasm, wasmMagic) {
		return nil, ErrNotWasm
	}
	inst, err := e.Start(ctx, "inspect", wasm, nil)
	if err != nil {
		return nil, err
	}
	inst.Stop()
	return inst.Hello(), nil
}
