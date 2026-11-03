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

package kv

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
)

// ErrNotFound is the error of a Get of a key that does not exist or has
// expired.
var ErrNotFound = errors.New("not found")

// ErrKeep is the error of an RPushTrim that would keep no element.
var ErrKeep = errors.New("keep must be at least 1")

// ErrConflict is the error of an Update whose context ended while it
// waited to try again after another writer changed the key.
var ErrConflict = errors.New("the key changed on every try")

// ErrNoDeadline is the error of an Update whose context has no deadline.
var ErrNoDeadline = errors.New("update needs a context with a deadline")

type KV interface {
	Has(ctx context.Context, key string) (bool, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
	// SetNX sets the key only if it does not already exist. Returns true if the
	// key was set, false if it was already present. The TTL is applied atomically.
	SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error)
	// RPush appends a value to a list stored under key. Returns the new length.
	RPush(ctx context.Context, key string, value []byte) (int64, error)
	// LDrain atomically returns all elements of the list and deletes the key.
	LDrain(ctx context.Context, key string) ([][]byte, error)
	// RPushTrim appends value to the list stored under key, keeps only its
	// newest keep elements and sets the TTL of the list to ttl, in one step.
	// A keep below 1 is ErrKeep.
	RPushTrim(ctx context.Context, key string, value []byte, keep int64, ttl time.Duration) error
	// LRange returns all elements of the list stored under key, oldest
	// first, without changing it. A list that does not exist is empty.
	LRange(ctx context.Context, key string) ([][]byte, error)
	// DeleteIfEqual deletes key only if its value equals value, in one
	// step, and reports whether it deleted it.
	DeleteIfEqual(ctx context.Context, key string, value []byte) (bool, error)
	// Update sets key to the value that fn returns for its current value,
	// nil for a key that does not exist or has expired, and sets its TTL
	// to ttl, in one step. fn must be pure: on Redis it runs again each
	// time another writer changes the key first, for as long as ctx lasts,
	// so the caller uses only the result of the last run before Update
	// returns nil, the run that was stored. A ctx without a deadline is
	// ErrNoDeadline on every store, so that no caller tries forever. An
	// error of fn stops Update, which returns it and changes nothing.
	// When ctx ends while Update waits to try again, Update returns
	// ErrConflict with the error of ctx and changes nothing.
	Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error
	Close() error
}

// Option changes how MakeKV makes a store.
type Option func(*options)

type options struct {
	clock     clock.Clock
	unbounded bool
}

// WithClock makes the in-memory store expire its keys on c. Redis keeps
// the TTLs of its keys itself.
func WithClock(c clock.Clock) Option {
	return func(o *options) { o.clock = c }
}

// WithoutCallBound makes a Redis store bound each call only by the context
// of its caller: it sets no deadline of its own on a call, nor on its reads
// and writes, and a call returns when its context ends, even when Redis
// does not answer. Tests use it, since a starved test machine can take longer
// than consts.RedisCallTimeout to run a goroutine whose answer has come;
// DMRHub keeps the bound.
func WithoutCallBound() Option {
	return func(o *options) { o.unbounded = true }
}

// MakeKV creates a new key-value store client.
func MakeKV(ctx context.Context, config *config.Config, opts ...Option) (KV, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	if config.Redis.Enabled {
		redisKV, err := makeRedisKV(ctx, config, o.unbounded)
		if err != nil {
			return nil, fmt.Errorf("failed to create redis kv: %w", err)
		}
		return redisKV, nil
	}

	return makeInMemoryKV(ctx, config, o.clock)
}
