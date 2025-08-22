// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeKVFromRedis(config *config.Config) (redisKV, error) {
	return redisKV{}, nil
}

type redisKV struct {
}

// const connsPerCPU = 10
// const maxIdleTime = 10 * time.Minute

// TODO: move this to pubsub and kv packages
// if cfg.Redis.Enabled {
// 	redis := redis.NewClient(&redis.Options{
// 		Addr:            fmt.Sprintf("%s:%d", cfg.Redis.Host, cfg.Redis.Port),
// 		Password:        cfg.Redis.Password,
// 		PoolFIFO:        true,
// 		PoolSize:        runtime.GOMAXPROCS(0) * connsPerCPU,
// 		MinIdleConns:    runtime.GOMAXPROCS(0),
// 		ConnMaxIdleTime: maxIdleTime,
// 	})
// 	_, err = redis.Ping(ctx).Result()
// 	if err != nil {
// 		return fmt.Errorf("failed to connect to redis: %w", err)
// 	}
// 	defer func() {
// 		err := redis.Close()
// 		if err != nil {
// 			slog.Error("Failed to close redis connection", "error", err)
// 		}
// 	}()
// 	if cfg.Metrics.OTLPEndpoint != "" {
// 		if err := redisotel.InstrumentTracing(redis); err != nil {
// 			return fmt.Errorf("failed to trace redis: %w", err)
// 		}

// 		// Enable metrics instrumentation.
// 		if err := redisotel.InstrumentMetrics(redis); err != nil {
// 			return fmt.Errorf("failed to instrument redis metrics: %w", err)
// 		}
// 	}
// }
