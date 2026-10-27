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
	"errors"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/monotime"
	"github.com/puzpuzpuz/xsync/v4"
)

// TTL is how old an answer gets before a read loads it again in the
// background.
const TTL = 5 * time.Second

// Idle is how long a Cache keeps an answer that nobody reads.
const Idle = 10 * time.Minute

// Wait is the longest that the reads of a key wait for its load, counted
// from the read that started it, so that a stalled database holds a burst
// for at most Wait.
const Wait = 500 * time.Millisecond

// ErrSlow is the error of a read that waited Wait for a load that did not
// end. The load goes on, and a later read gets its answer.
var ErrSlow = errors.New("the lookup is waiting on the database")

// Cache holds the answers of load by key. A read of a stale answer gets it
// at once and starts a background load that replaces it, so a read waits
// on load only for a key that nobody read before, and then at most Wait.
type Cache[K comparable, V any] struct {
	name    string
	ttl     time.Duration
	load    func(K) (V, error)
	entries *xsync.Map[K, *entry[V]]
	flights *Flights[K, V]
}

// Flights runs one load of each key at a time, in the background.
type Flights[K comparable, V any] struct {
	m *xsync.Map[K, *flight[V]]
}

type flight[V any] struct {
	done  chan struct{}
	until time.Time
	val   V
	err   error
}

// NewFlights returns Flights with no load running.
func NewFlights[K comparable, V any]() *Flights[K, V] {
	return &Flights[K, V]{m: xsync.NewMap[K, *flight[V]]()}
}

// Do returns the answer of load for key. It starts load when no load of
// key runs, and waits for the load until Wait after it started; then it
// returns ErrSlow.
func (f *Flights[K, V]) Do(key K, load func() (V, error)) (V, error) {
	fl := f.start(key, load)
	select {
	case <-fl.done:
		return fl.val, fl.err
	default:
	}
	t := time.NewTimer(time.Until(fl.until))
	defer t.Stop()
	select {
	case <-fl.done:
		return fl.val, fl.err
	case <-t.C:
		var zero V
		return zero, ErrSlow
	}
}

// DoWait is Do that waits for the load to end.
func (f *Flights[K, V]) DoWait(key K, load func() (V, error)) (V, error) {
	fl := f.start(key, load)
	<-fl.done
	return fl.val, fl.err
}

func (f *Flights[K, V]) start(key K, load func() (V, error)) *flight[V] {
	fl, loaded := f.m.LoadOrCompute(key, func() (*flight[V], bool) {
		return &flight[V]{done: make(chan struct{}), until: time.Now().Add(Wait)}, false
	})
	if !loaded {
		go func() {
			fl.val, fl.err = load()
			f.m.Delete(key)
			close(fl.done)
		}()
	}
	return fl
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
	return &Cache[K, V]{name: name, ttl: ttl, load: load, entries: xsync.NewMap[K, *entry[V]](), flights: NewFlights[K, V]()}
}

// Get returns the answer for key. It calls load only when it has no
// answer for key; an error of that load is returned and not kept. A Get
// that waited Wait for the load returns ErrSlow.
func (c *Cache[K, V]) Get(key K) (V, error) {
	now := time.Now()
	if e, ok := c.entries.Load(key); ok {
		e.read.Store(monotime.Of(now))
		if now.Sub(e.at) >= c.ttl && e.refreshing.CompareAndSwap(false, true) {
			go c.refresh(key, e)
		}
		return e.val, nil
	}
	return c.flights.Do(key, func() (V, error) {
		v, err := c.load(key)
		if err == nil {
			c.entries.LoadOrStore(key, newEntry(v, time.Now()))
		}
		return v, err
	})
}

func newEntry[V any](v V, now time.Time) *entry[V] {
	e := &entry[V]{val: v, at: now}
	e.read.Store(monotime.Of(now))
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

// Reload loads the answer for key again when the Cache holds one, and
// keeps the new answer. When the load fails, the Cache forgets the answer,
// so that the next Get loads it.
func (c *Cache[K, V]) Reload(key K) {
	if _, ok := c.entries.Load(key); !ok {
		return
	}
	v, err := c.load(key)
	if err != nil {
		slog.Error("Cannot reload a changed lookup, forgetting it", "lookup", c.name, "error", err)
		c.entries.Delete(key)
		return
	}
	c.entries.Store(key, newEntry(v, time.Now()))
}

// Refresh loads the answer for key again when the Cache holds one, and
// keeps the new answer, so that an answer that is read seldom is not
// pruned. When the load fails, the Cache keeps the answer that it had.
func (c *Cache[K, V]) Refresh(key K) error {
	if _, ok := c.entries.Load(key); !ok {
		return nil
	}
	v, err := c.load(key)
	if err != nil {
		return err
	}
	c.Put(key, v)
	return nil
}

// Put keeps v as the answer for key.
func (c *Cache[K, V]) Put(key K, v V) {
	c.entries.Store(key, newEntry(v, time.Now()))
}

// Drop forgets the answer for key, so that the next Get loads it.
func (c *Cache[K, V]) Drop(key K) {
	c.entries.Delete(key)
}

// Prune forgets the answers that nobody read for Idle before now.
func (c *Cache[K, V]) Prune(now time.Time) {
	c.entries.Range(func(key K, e *entry[V]) bool {
		if now.Sub(monotime.Time(e.read.Load())) > Idle {
			c.entries.Delete(key)
		}
		return true
	})
}
