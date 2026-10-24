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

// Command parrot is a DMRHub plugin that plays each private voice call to
// its IDs back to the caller. Add it on the Plugins page of the hub with the
// parrot's ID, such as 9990, then run it with the address, token and
// fingerprint that the page shows:
//
//	DMRHUB_PLUGIN_TOKEN=dmrhub_plg_... parrot -address hub.example.org:3006 -fingerprint 4F:2A:...
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

const (
	minBackoff = time.Second
	maxBackoff = 30 * time.Second
)

func main() {
	address := flag.String("address", os.Getenv("DMRHUB_PLUGIN_ADDRESS"), "address of the hub's plugin listener, as host:port")
	fingerprint := flag.String("fingerprint", os.Getenv("DMRHUB_PLUGIN_FINGERPRINT"),
		"SHA-256 of the hub's own certificate, from its Plugins page; empty for a certificate from a certificate authority")
	flag.Parse()
	token := os.Getenv("DMRHUB_PLUGIN_TOKEN")
	if *address == "" || token == "" {
		slog.Error("Set -address (or DMRHUB_PLUGIN_ADDRESS) and DMRHUB_PLUGIN_TOKEN")
		os.Exit(2)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	run(ctx, plugin.Options{Address: *address, Token: token, Fingerprint: *fingerprint})
}

// run keeps the parrot connected until ctx ends, connecting again after a
// wait that doubles up to maxBackoff.
func run(ctx context.Context, o plugin.Options) {
	backoff := minBackoff
	for ctx.Err() == nil {
		started := time.Now()
		err := serve(ctx, o)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("Parrot: disconnected", "error", err)
		if time.Since(started) > maxBackoff {
			backoff = minBackoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func serve(ctx context.Context, o plugin.Options) error {
	conn, err := plugin.Dial(ctx, o)
	if err != nil {
		return fmt.Errorf("cannot connect: %w", err)
	}
	defer func() { _ = conn.Close() }()
	slog.Info("Parrot: connected", "name", conn.Name(), "ids", conn.Ranges())
	p := NewParrot(conn.Send)
	defer p.Wait()
	go func() {
		<-ctx.Done()
		_ = conn.Close()
	}()
	for {
		m, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive: %w", err)
		}
		switch msg := m.GetMessage().(type) {
		case *pluginv1.ConnectResponse_Burst:
			p.Handle(msg.Burst)
		case *pluginv1.ConnectResponse_Refusal:
			slog.Warn("Parrot: the hub refused a burst", "stream", msg.Refusal.GetStreamId(), "reason", msg.Refusal.GetReason())
		case *pluginv1.ConnectResponse_Ranges:
			slog.Info("Parrot: new IDs", "ids", msg.Ranges.GetRanges())
		case *pluginv1.ConnectResponse_Welcome:
		default:
			return errors.New("unknown message from the hub")
		}
	}
}
