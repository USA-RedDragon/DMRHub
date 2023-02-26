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

package hbrp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

type redisClient struct {
	Redis *redis.Client
}

var (
	errNoSuchRepeater    = errors.New("no such repeater")
	errUnmarshalRepeater = errors.New("unmarshal repeater")
	errCastRepeater      = errors.New("unable to cast repeater id")
)

const repeaterExpireTime = 5 * time.Minute

func makeRedisClient(redis *redis.Client) redisClient {
	return redisClient{
		Redis: redis,
	}
}

func (s *redisClient) updateRepeaterPing(ctx context.Context, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.updateRepeaterPing")
	defer span.End()

	repeater, err := s.getRepeater(ctx, repeaterID)
	if err != nil {
		logging.Errorf("Error getting repeater from redis: %v", err)
		return
	}
	repeater.LastPing = time.Now()
	s.storeRepeater(ctx, repeaterID, repeater)
	s.Redis.Expire(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterExpireTime)
}

func (s *redisClient) updateRepeaterConnection(ctx context.Context, repeaterID uint, connection string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.updateRepeaterConnection")
	defer span.End()

	repeater, err := s.getRepeater(ctx, repeaterID)
	if err != nil {
		logging.Errorf("Error getting repeater from redis: %v", err)
		return
	}
	repeater.Connection = connection
	s.storeRepeater(ctx, repeaterID, repeater)
}

func (s *redisClient) deleteRepeater(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.deleteRepeater")
	defer span.End()

	return s.Redis.Del(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Val() == 1
}

func (s *redisClient) storeRepeater(ctx context.Context, repeaterID uint, repeater models.Repeater) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.storeRepeater")
	defer span.End()

	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		logging.Errorf("Error marshalling repeater: %v", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterBytes, repeaterExpireTime)
}

func (s *redisClient) getRepeater(ctx context.Context, repeaterID uint) (models.Repeater, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.getRepeater")
	defer span.End()

	repeaterBits, err := s.Redis.Get(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Result()
	if err != nil {
		logging.Errorf("Error getting repeater from redis: %v", err)
		return models.Repeater{}, errNoSuchRepeater
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg([]byte(repeaterBits))
	if err != nil {
		logging.Errorf("Error unmarshalling repeater: %v", err)
		return models.Repeater{}, errUnmarshalRepeater
	}
	return repeater, nil
}

func (s *redisClient) repeaterExists(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.repeaterExists")
	defer span.End()

	return s.Redis.Exists(ctx, fmt.Sprintf("hbrp:repeater:%d", repeaterID)).Val() == 1
}

func (s *redisClient) listRepeaters(ctx context.Context) ([]uint, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisClient.listRepeaters")
	defer span.End()

	var cursor uint64
	var repeaters []uint
	for {
		keys, _, err := s.Redis.Scan(ctx, cursor, "hbrp:repeater:*", 0).Result()
		if err != nil {
			return nil, errNoSuchRepeater
		}
		for _, key := range keys {
			repeaterNum, err := strconv.Atoi(strings.Replace(key, "hbrp:repeater:", "", 1))
			if err != nil {
				return nil, errCastRepeater
			}
			repeaters = append(repeaters, uint(repeaterNum))
		}

		if cursor == 0 {
			break
		}
	}
	return repeaters, nil
}
