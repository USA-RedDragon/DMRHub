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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

type KV interface {
	Has(ctx context.Context, key string) (bool, error)
	Get(ctx context.Context, key string) ([]byte, error)
	Set(ctx context.Context, key string, value []byte) error
	Delete(ctx context.Context, key string) error
	Expire(ctx context.Context, key string, ttl time.Duration) error
	Scan(ctx context.Context, cursor uint64, match string, count int64) ([]string, uint64, error)
	// RPush appends a value to a list stored under key. Returns the new length.
	RPush(ctx context.Context, key string, value []byte) (int64, error)
	// LDrain atomically returns all elements of the list and deletes the key.
	LDrain(ctx context.Context, key string) ([][]byte, error)
	Close() error
}

// MakeKV creates a new key-value store client.
func MakeKV(ctx context.Context, config *config.Config) (KV, error) {
	if config.Redis.Enabled {
		redisKV, err := makeRedisKV(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("failed to create redis kv: %w", err)
		}
		return redisKV, nil
	}

	return makeInMemoryKV(ctx, config)
}
