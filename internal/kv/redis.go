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
	"fmt"
	"runtime"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func makeRedisKV(ctx context.Context, config *config.Config) (KV, error) {
	client := redis.NewClient(&redis.Options{
		Addr:            fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port),
		Password:        config.Redis.Password,
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * consts.ConnsPerCPU,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: consts.MaxIdleTime,
	})

	if _, err := client.Ping(ctx).Result(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	if config.Metrics.OTLPEndpoint != "" {
		if err := redisotel.InstrumentTracing(client); err != nil {
			return nil, fmt.Errorf("failed to trace redis: %w", err)
		}
		if err := redisotel.InstrumentMetrics(client); err != nil {
			return nil, fmt.Errorf("failed to instrument redis metrics: %w", err)
		}
	}

	return &redisKV{client: client}, nil
}

type redisKV struct {
	client *redis.Client
}

func (kv *redisKV) Has(key string) (bool, error) {
	result, err := kv.client.Exists(context.Background(), key).Result()
	if err != nil {
		return false, fmt.Errorf("redis exists %s: %w", key, err)
	}
	return result > 0, nil
}

func (kv *redisKV) Get(key string) ([]byte, error) {
	value, err := kv.client.Get(context.Background(), key).Bytes()
	if err != nil {
		return nil, fmt.Errorf("redis get %s: %w", key, err)
	}
	return value, nil
}

func (kv *redisKV) Set(key string, value []byte) error {
	if err := kv.client.Set(context.Background(), key, value, 0).Err(); err != nil {
		return fmt.Errorf("redis set %s: %w", key, err)
	}
	return nil
}

func (kv *redisKV) Delete(key string) error {
	if err := kv.client.Del(context.Background(), key).Err(); err != nil {
		return fmt.Errorf("redis delete %s: %w", key, err)
	}
	return nil
}

func (kv *redisKV) Expire(key string, ttl time.Duration) error {
	result, err := kv.client.Expire(context.Background(), key, ttl).Result()
	if err != nil {
		return fmt.Errorf("redis expire %s: %w", key, err)
	}
	if !result {
		return fmt.Errorf("key %s not found", key)
	}
	return nil
}

func (kv *redisKV) Scan(cursor uint64, match string, count int64) ([]string, uint64, error) {
	keys, next, err := kv.client.Scan(context.Background(), cursor, match, count).Result()
	if err != nil {
		return nil, 0, fmt.Errorf("redis scan match %s: %w", match, err)
	}
	return keys, next, nil
}

func (kv *redisKV) Close() error {
	if err := kv.client.Close(); err != nil {
		return fmt.Errorf("redis close: %w", err)
	}
	return nil
}
