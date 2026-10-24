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

package pubsub

import (
	"context"
	"fmt"
	"runtime"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func makePubSubFromRedis(ctx context.Context, config *config.Config) (ret redisPubSub, err error) {
	redis := redis.NewClient(&redis.Options{
		Addr:            fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port),
		Password:        config.Redis.Password,
		PoolFIFO:        true,
		PoolSize:        runtime.GOMAXPROCS(0) * consts.ConnsPerCPU,
		MinIdleConns:    runtime.GOMAXPROCS(0),
		ConnMaxIdleTime: consts.MaxIdleTime,
	})
	_, err = redis.Ping(ctx).Result()
	if err != nil {
		err = fmt.Errorf("failed to connect to redis: %w", err)
		return
	}

	if config.Metrics.OTLPEndpoint != "" {
		if err = redisotel.InstrumentTracing(redis); err != nil {
			err = fmt.Errorf("failed to trace redis: %w", err)
			return
		}

		// Enable metrics instrumentation.
		if err = redisotel.InstrumentMetrics(redis); err != nil {
			err = fmt.Errorf("failed to instrument redis metrics: %w", err)
			return
		}
	}

	return redisPubSub{client: redis}, nil
}

type redisPubSub struct {
	client *redis.Client
}

func (ps redisPubSub) Publish(topic string, message []byte) error {
	ctx := context.Background()
	if err := ps.client.Publish(ctx, topic, message).Err(); err != nil {
		return fmt.Errorf("failed to publish message to topic %s: %w", topic, err)
	}
	return nil
}

func (ps redisPubSub) Subscribe(topic string) Subscription {
	ctx := context.Background()
	sub := ps.client.Subscribe(ctx, topic)
	in := sub.Channel()
	out := make(chan []byte)
	done := make(chan struct{})
	go func() {
		defer close(out)
		for msg := range in {
			select {
			case out <- []byte(msg.Payload):
			case <-done:
				return
			}
		}
	}()
	return redisSubscription{ch: out, sub: sub, done: done, once: &sync.Once{}}
}

func (ps redisPubSub) Close() error {
	if err := ps.client.Close(); err != nil {
		return fmt.Errorf("failed to close redis client: %w", err)
	}
	return nil
}

// redisSubscription forwards the messages of sub on ch, which every call of
// Channel returns, so that asking for it again loses nothing.
type redisSubscription struct {
	ch   <-chan []byte
	sub  *redis.PubSub
	done chan struct{}
	once *sync.Once
}

func (s redisSubscription) Close() error {
	s.once.Do(func() { close(s.done) })
	if err := s.sub.Close(); err != nil {
		return fmt.Errorf("failed to close redis subscription: %w", err)
	}
	return nil
}

func (s redisSubscription) Channel() <-chan []byte {
	return s.ch
}
