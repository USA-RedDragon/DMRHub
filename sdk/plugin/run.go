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

package plugin

import (
	"context"
	"log/slog"
	"time"
)

const (
	// minBackoff is the first wait before connecting again.
	minBackoff = time.Second
	// maxBackoff is the longest wait before connecting again. A connection
	// that lasted longer starts the waits over.
	maxBackoff = 30 * time.Second
)

// Run keeps the plugin connected to its hub until ctx ends. It calls serve
// with each connection, and closes the connection when serve returns or ctx
// ends, which makes a blocked Recv return. It then connects again, after a
// wait that doubles from a second up to 30 seconds.
func Run(ctx context.Context, o Options, serve func(context.Context, *Conn) error) {
	run(ctx, func(ctx context.Context) (*Conn, error) { return Dial(ctx, o) }, serve, sleep, time.Now)
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func run(ctx context.Context, dial func(context.Context) (*Conn, error), serve func(context.Context, *Conn) error,
	wait func(context.Context, time.Duration) bool, now func() time.Time) {
	backoff := minBackoff
	for ctx.Err() == nil {
		started := now()
		err := connect(ctx, dial, serve)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("Plugin: disconnected from the hub", "error", err)
		if now().Sub(started) > maxBackoff {
			backoff = minBackoff
		}
		if !wait(ctx, backoff) {
			return
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func connect(ctx context.Context, dial func(context.Context) (*Conn, error), serve func(context.Context, *Conn) error) error {
	conn, err := dial(ctx)
	if err != nil {
		return err
	}
	slog.Info("Plugin: connected to the hub", "name", conn.Name(), "ids", conn.Ranges())
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer func() {
		stop()
		_ = conn.Close()
	}()
	return serve(ctx, conn)
}
