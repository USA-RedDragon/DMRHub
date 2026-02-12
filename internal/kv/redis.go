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

var claimLeaseScript = redis.NewScript(`
local owner = ARGV[1]
local ttlMs = tonumber(ARGV[2])

local current = redis.call('GET', KEYS[1])
if not current then
	redis.call('PSETEX', KEYS[1], ttlMs, owner)
	return 1
end

if current == owner then
	redis.call('PEXPIRE', KEYS[1], ttlMs)
	return 1
end

return 0
`)

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
		return false, err
	}
	return result > 0, nil
}

func (kv *redisKV) Get(key string) ([]byte, error) {
	value, err := kv.client.Get(context.Background(), key).Bytes()
	if err != nil {
		return nil, err
	}
	return value, nil
}

func (kv *redisKV) Set(key string, value []byte) error {
	return kv.client.Set(context.Background(), key, value, 0).Err()
}

func (kv *redisKV) ClaimLease(key string, owner string, ttl time.Duration) (bool, error) {
	if ttl <= 0 {
		return false, fmt.Errorf("ttl must be positive")
	}

	claim, err := claimLeaseScript.Run(context.Background(), kv.client, []string{key}, owner, ttl.Milliseconds()).Int()
	if err != nil {
		return false, err
	}

	return claim == 1, nil
}

func (kv *redisKV) Delete(key string) error {
	return kv.client.Del(context.Background(), key).Err()
}

func (kv *redisKV) Expire(key string, ttl time.Duration) error {
	result, err := kv.client.Expire(context.Background(), key, ttl).Result()
	if err != nil {
		return err
	}
	if !result {
		return fmt.Errorf("key %s not found", key)
	}
	return nil
}

func (kv *redisKV) Scan(cursor uint64, match string, count int64) ([]string, uint64, error) {
	return kv.client.Scan(context.Background(), cursor, match, count).Result()
}

func (kv *redisKV) Close() error {
	return kv.client.Close()
}
