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

	"github.com/redis/go-redis/v9"
)

// FillRedisPool opens and sets up every connection that the pool of a Redis
// store can hold, then gives them all back to the pool as idle connections.
func FillRedisPool(ctx context.Context, store KV) error {
	r, ok := store.(*redisKV)
	if !ok {
		return errors.New("not a Redis store")
	}
	conns := make([]*redis.Conn, 0, r.client.Options().PoolSize)
	defer func() {
		for _, conn := range conns {
			_ = conn.Close()
		}
	}()
	for range cap(conns) {
		conn := r.client.Conn()
		conns = append(conns, conn)
		if err := conn.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("ping a pooled connection: %w", err)
		}
	}
	return nil
}

// ExpiryOf returns when the key of store expires.
func ExpiryOf(ctx context.Context, store KV, key string) (time.Time, error) {
	switch s := store.(type) {
	case *redisKV:
		d, err := s.client.PTTL(ctx, key).Result()
		if err != nil {
			return time.Time{}, fmt.Errorf("redis pttl %s: %w", key, err)
		}
		return time.Now().Add(d), nil
	case inMemoryKV:
		v, ok := s.kv.Load(key)
		if !ok {
			return time.Time{}, ErrNotFound
		}
		return v.ttl, nil
	}
	return time.Time{}, errors.New("unknown store")
}

// UpdateTries is how many times Update tries on Redis.
const UpdateTries = updateTries

func PingUntil(ctx context.Context, ping func(context.Context) error) error {
	return pingUntil(ctx, ping)
}
