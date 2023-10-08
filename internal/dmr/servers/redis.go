// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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

package servers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"k8s.io/klog/v2"
)

type RedisClient struct {
	Redis *redis.Client
}

var (
	ErrNoSuchRepeater    = errors.New("no such repeater")
	ErrUnmarshalRepeater = errors.New("unmarshal repeater")
	ErrCastRepeater      = errors.New("unable to cast repeater id")
	ErrNoSuchPeer        = errors.New("no such peer")
	ErrUnmarshalPeer     = errors.New("unmarshal peer")
)

const repeaterExpireTime = 5 * time.Minute

func MakeRedisClient(redis *redis.Client) *RedisClient {
	return &RedisClient{
		Redis: redis,
	}
}

func (s *RedisClient) UpdateRepeaterPing(ctx context.Context, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.updateRepeaterPing")
	defer span.End()

	repeater, err := s.GetRepeater(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.LastPing = time.Now()
	s.StoreRepeater(ctx, repeaterID, repeater)
	s.Redis.Expire(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterExpireTime)
}

func (s *RedisClient) UpdateRepeaterConnection(ctx context.Context, repeaterID uint, connection string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.updateRepeaterConnection")
	defer span.End()

	repeater, err := s.GetRepeater(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.Connection = connection
	s.StoreRepeater(ctx, repeaterID, repeater)
}

func (s *RedisClient) DeleteRepeater(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.deleteRepeater")
	defer span.End()

	return s.Redis.Del(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Val() == 1
}

func (s *RedisClient) StoreRepeater(ctx context.Context, repeaterID uint, repeater models.Repeater) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.storeRepeater")
	defer span.End()

	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling repeater", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterBytes, repeaterExpireTime)
}

func (s *RedisClient) GetRepeater(ctx context.Context, repeaterID uint) (models.Repeater, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.getRepeater")
	defer span.End()

	repeaterBits, err := s.Redis.Get(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Result()
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return models.Repeater{}, ErrNoSuchRepeater
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg([]byte(repeaterBits))
	if err != nil {
		klog.Errorf("Error unmarshalling repeater", err)
		return models.Repeater{}, ErrUnmarshalRepeater
	}
	return repeater, nil
}

func (s *RedisClient) RepeaterExists(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.repeaterExists")
	defer span.End()

	return s.Redis.Exists(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Val() == 1
}

func (s *RedisClient) ListRepeaters(ctx context.Context) ([]uint, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.listRepeaters")
	defer span.End()

	var cursor uint64
	var repeaters []uint
	for {
		keys, _, err := s.Redis.Scan(ctx, cursor, "hbrp:repeater:*", 0).Result()
		if err != nil {
			return nil, ErrNoSuchRepeater
		}
		for _, key := range keys {
			repeaterNum, err := strconv.Atoi(strings.Replace(key, "hbrp:repeater:", "", 1))
			if err != nil {
				return nil, ErrCastRepeater
			}
			repeaters = append(repeaters, uint(repeaterNum))
		}

		if cursor == 0 {
			break
		}
	}
	return repeaters, nil
}

func (s *RedisClient) GetPeer(ctx context.Context, peerID uint) (models.Peer, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	peerBits, err := s.Redis.Get(ctx, fmt.Sprintf("openbridge:peer:%d", peerID)).Result()
	if err != nil {
		klog.Errorf("Error getting peer from redis", err)
		return models.Peer{}, ErrNoSuchPeer
	}
	var peer models.Peer
	_, err = peer.UnmarshalMsg([]byte(peerBits))
	if err != nil {
		klog.Errorf("Error unmarshalling peer", err)
		return models.Peer{}, ErrUnmarshalPeer
	}
	return peer, nil
}
