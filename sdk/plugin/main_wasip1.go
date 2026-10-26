// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build wasip1

package plugin

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// Main runs a plugin inside DMRHub, built with GOOS=wasip1 GOARCH=wasm. It
// says hello to the hub with no name or IDs of its own; use MainWith to
// give them.
func Main(serve func(context.Context, *Conn) error) {
	MainWith(nil, serve)
}

// MainWith runs a plugin inside DMRHub, built with GOOS=wasip1
// GOARCH=wasm, which tells the hub who it is with hello: its name, version,
// author, description, the IDs it asks for and the hosts it asks to reach.
// It calls serve with the connection to the hub, and exits when serve
// returns, with status 1 when serve fails. The hub starts the plugin again.
func MainWith(hello *pluginv1.Hello, serve func(context.Context, *Conn) error) {
	if err := setNonblock(0); err != nil {
		fmt.Fprintln(os.Stderr, "plugin: stdin stays blocking:", err)
	}
	s := newStdio(os.NewFile(0, "stdin"), os.Stdout)
	welcome, err := handshake(s, helloOf(hello))
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1) //nolint:revive // the plugin ends, and the hub starts it again
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-s.done
		cancel()
	}()
	if err := serve(ctx, &Conn{s: s, welcome: welcome}); err != nil {
		slog.Error("Plugin: stopped", "error", err)
		cancel()
		os.Exit(1) //nolint:revive,gocritic // the plugin ends, and the hub starts it again
	}
	cancel()
}
