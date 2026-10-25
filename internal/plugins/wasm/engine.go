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
	"errors"
	"fmt"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/tetratelabs/wazero"
)

var (
	// ErrNotWasm is returned for a file that is not a WebAssembly module
	// that DMRHub can run.
	ErrNotWasm = errors.New("not a WebAssembly module for wasip1")
	// ErrNoHello is returned for a module that does not start with a hello
	// in time, as a plugin does.
	ErrNoHello = errors.New("the module did not say hello as a plugin")
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

// Engine runs the plugins inside DMRHub. Each start compiles the plugin
// again, so that the code of an old version, or of a file that an admin
// only checked, is freed with its plugin.
type Engine struct {
	cache  wazero.CompilationCache
	limits Limits
}

// NewEngine returns an Engine whose plugins run within limits.
func NewEngine(limits Limits) *Engine {
	return &Engine{limits: limits}
}

// NewCachedEngine returns an Engine whose plugins run within limits, and
// that keeps every module it compiled until Close, so that the same module
// starts again fast. The tests use it.
func NewCachedEngine(limits Limits) *Engine {
	return &Engine{cache: wazero.NewCompilationCache(), limits: limits}
}

// Limited returns an Engine with the cache of e, if any, whose plugins run
// within limits.
func (e *Engine) Limited(limits Limits) *Engine {
	return &Engine{cache: e.cache, limits: limits}
}

// Close frees the compiled modules of the cache.
func (e *Engine) Close(ctx context.Context) error {
	if e.cache == nil {
		return nil
	}
	if err := e.cache.Close(ctx); err != nil {
		return fmt.Errorf("could not free the compiled plugins: %w", err)
	}
	return nil
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
