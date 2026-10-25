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

// Package lookup keeps the answers of the database that the forwarding
// path reads, so that a burst does not wait on the database.
package lookup

import (
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
)

// TTL is how old an answer gets before a read loads it again in the
// background.
const TTL = 5 * time.Second

// Idle is how long a Cache keeps an answer that nobody reads.
const Idle = 10 * time.Minute

// Cache holds the answers of load by key. A read of a stale answer gets it
// at once and starts a background load that replaces it, so a read waits
// on load only for a key that nobody read before.
type Cache[K comparable, V any] struct {
	name    string
	ttl     time.Duration
	load    func(K) (V, error)
	entries *xsync.Map[K, *entry[V]]
}

type entry[V any] struct {
	val        V
	at         time.Time
	read       atomic.Int64
	refreshing atomic.Bool
}

// New returns a Cache that loads an answer with load and keeps it fresh
// for ttl. name names the cache in logs.
func New[K comparable, V any](name string, ttl time.Duration, load func(K) (V, error)) *Cache[K, V] {
	return &Cache[K, V]{name: name, ttl: ttl, load: load, entries: xsync.NewMap[K, *entry[V]]()}
}

// Get returns the answer for key. It calls load only when it has no
// answer for key; an error of that load is returned and not kept.
func (c *Cache[K, V]) Get(key K) (V, error) {
	now := time.Now()
	if e, ok := c.entries.Load(key); ok {
		e.read.Store(now.UnixNano())
		if now.Sub(e.at) >= c.ttl && e.refreshing.CompareAndSwap(false, true) {
			go c.refresh(key, e)
		}
		return e.val, nil
	}
	v, err := c.load(key)
	if err != nil {
		return v, err
	}
	c.entries.LoadOrStore(key, newEntry(v, now))
	return v, nil
}

func newEntry[V any](v V, now time.Time) *entry[V] {
	e := &entry[V]{val: v, at: now}
	e.read.Store(now.UnixNano())
	return e
}

func (c *Cache[K, V]) refresh(key K, old *entry[V]) {
	v, err := c.load(key)
	if err != nil {
		slog.Error("Cannot reload a lookup, keeping the old answer", "lookup", c.name, "error", err)
		v = old.val
	}
	next := newEntry(v, time.Now())
	next.read.Store(old.read.Load())
	c.entries.Compute(key, func(cur *entry[V], loaded bool) (*entry[V], xsync.ComputeOp) {
		if loaded && cur == old {
			return next, xsync.UpdateOp
		}
		return cur, xsync.CancelOp
	})
}

// Drop forgets the answer for key, so that the next Get loads it.
func (c *Cache[K, V]) Drop(key K) {
	c.entries.Delete(key)
}

// Prune forgets the answers that nobody read for Idle before now.
func (c *Cache[K, V]) Prune(now time.Time) {
	c.entries.Range(func(key K, e *entry[V]) bool {
		if now.Sub(time.Unix(0, e.read.Load())) > Idle {
			c.entries.Delete(key)
		}
		return true
	})
}
