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
	"log/slog"
	"runtime"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

func makePubSubFromRedis(ctx context.Context, config *config.Config) (ret redisPubSub, err error) {
	redis := redis.NewClient(&redis.Options{
		Addr:                  fmt.Sprintf("%s:%d", config.Redis.Host, config.Redis.Port),
		Password:              config.Redis.Password,
		PoolFIFO:              true,
		PoolSize:              runtime.GOMAXPROCS(0) * consts.ConnsPerCPU,
		MinIdleConns:          runtime.GOMAXPROCS(0),
		ConnMaxIdleTime:       consts.MaxIdleTime,
		DialTimeout:           consts.RedisDialTimeout,
		ReadTimeout:           consts.RedisTimeout,
		WriteTimeout:          consts.RedisTimeout,
		ContextTimeoutEnabled: true,
		MaxRetries:            -1,
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

	return redisPubSub{client: redis, feeds: &feedSet{all: map[*redisFeed]struct{}{}}}, nil
}

type redisPubSub struct {
	client *redis.Client
	feeds  *feedSet
}

// feedSet is the open feeds of a redisPubSub, which Close closes.
type feedSet struct {
	mu  sync.Mutex
	all map[*redisFeed]struct{}
}

func (ps redisPubSub) Publish(topic string, message []byte) error {
	ctx := context.Background()
	if err := ps.client.Publish(ctx, topic, message).Err(); err != nil {
		return fmt.Errorf("failed to publish message to topic %s: %w", topic, err)
	}
	return nil
}

// subscribeWait is the longest time Subscribe waits for Redis to confirm a
// subscription.
const subscribeWait = time.Second

// Subscribe subscribes to topic and returns once Redis confirms it, so
// that every message published after it returns comes. When Redis does
// not confirm it in time, the subscription still starts, as go-redis
// subscribes again on its own.
func (ps redisPubSub) Subscribe(topic string) Subscription {
	ctx := context.Background()
	sub := ps.client.Subscribe(ctx, topic)
	wait, cancel := context.WithTimeout(ctx, subscribeWait)
	if _, err := sub.Receive(wait); err != nil {
		slog.Warn("Redis did not confirm a subscription", "topic", topic, "error", err)
	}
	cancel()
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
	ps.feeds.mu.Lock()
	feeds := make([]*redisFeed, 0, len(ps.feeds.all))
	for f := range ps.feeds.all {
		feeds = append(feeds, f)
	}
	ps.feeds.mu.Unlock()
	for _, f := range feeds {
		_ = f.Close()
	}
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

// Feed returns a Feed on one Redis connection, which gets the messages of
// all its topics in the order Redis took them.
func (ps redisPubSub) Feed() Feed {
	sub := ps.client.Subscribe(context.Background())
	f := &redisFeed{sub: sub, set: ps.feeds, ch: make(chan Message, feedQueue), done: make(chan struct{}), waits: map[string][]chan struct{}{}}
	ps.feeds.mu.Lock()
	ps.feeds.all[f] = struct{}{}
	ps.feeds.mu.Unlock()
	in := sub.ChannelWithSubscriptions()
	go func() {
		defer close(f.ch)
		for v := range in {
			switch m := v.(type) {
			case *redis.Subscription:
				if m.Kind == "subscribe" {
					f.confirm(m.Channel)
				}
			case *redis.Message:
				select {
				case f.ch <- Message{Topic: m.Channel, Payload: []byte(m.Payload)}:
				case <-f.done:
					return
				}
			}
		}
	}()
	return f
}

type redisFeed struct {
	sub   *redis.PubSub
	set   *feedSet
	ch    chan Message
	done  chan struct{}
	once  sync.Once
	mu    sync.Mutex
	waits map[string][]chan struct{}
}

func (f *redisFeed) confirm(topic string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, w := range f.waits[topic] {
		close(w)
	}
	delete(f.waits, topic)
}

// Add subscribes to topic and waits for Redis to confirm it, for at most
// subscribeWait.
func (f *redisFeed) Add(topic string) {
	w := make(chan struct{})
	f.mu.Lock()
	f.waits[topic] = append(f.waits[topic], w)
	f.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), subscribeWait)
	defer cancel()
	if err := f.sub.Subscribe(ctx, topic); err != nil {
		slog.Warn("Failed to subscribe a feed", "topic", topic, "error", err)
		return
	}
	select {
	case <-w:
	case <-ctx.Done():
		slog.Warn("Redis did not confirm a subscription", "topic", topic, "error", ctx.Err())
	case <-f.done:
	}
}

func (f *redisFeed) Remove(topic string) {
	ctx, cancel := context.WithTimeout(context.Background(), subscribeWait)
	defer cancel()
	if err := f.sub.Unsubscribe(ctx, topic); err != nil {
		slog.Warn("Failed to unsubscribe a feed", "topic", topic, "error", err)
	}
}

func (f *redisFeed) Channel() <-chan Message {
	return f.ch
}

func (f *redisFeed) Close() error {
	f.once.Do(func() { close(f.done) })
	f.set.mu.Lock()
	delete(f.set.all, f)
	f.set.mu.Unlock()
	if err := f.sub.Close(); err != nil {
		return fmt.Errorf("failed to close redis feed: %w", err)
	}
	return nil
}
