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

// Package monotime keeps a time in an int64 with its monotonic clock

package clock

import (
	"context"
	"sync"
	"time"
)

// WithTimeout is context.WithTimeout on the time of c. On a Clock that is
// not Real, context.Cause of the context is the cause of parent.
func WithTimeout(parent context.Context, c Clock, d time.Duration) (context.Context, context.CancelFunc) {
	if IsReal(c) {
		return context.WithTimeout(parent, d)
	}
	ctx := &timeoutCtx{Context: parent, deadline: c.Now().Add(d), done: make(chan struct{})}
	t := c.AfterFunc(d, func() { ctx.end(context.DeadlineExceeded) })
	stop := context.AfterFunc(parent, func() { ctx.end(parent.Err()) })
	return ctx, func() {
		t.Stop()
		stop()
		ctx.end(context.Canceled)
	}
}

type timeoutCtx struct {
	context.Context //nolint:containedctx // the parent, for its values
	deadline        time.Time
	done            chan struct{}
	once            sync.Once
	mu              sync.Mutex
	err             error
}

func (c *timeoutCtx) end(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
	})
}

func (c *timeoutCtx) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func (c *timeoutCtx) Done() <-chan struct{} {
	return c.done
}

func (c *timeoutCtx) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.err
}
