// SPDX-License-Identifier: AGPL-3.0-only
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

package dmr

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"k8s.io/klog/v2"
)

type redisRepeaterStorage struct {
	Redis *redis.Client
}

var (
	ErrNoSuchRepeater    = errors.New("no such repeater")
	ErrUnmarshalRepeater = errors.New("unmarshal repeater")
	ErrCastRepeater      = errors.New("unable to cast repeater id")
)

const repeaterExpireTime = 5 * time.Minute

func makeRedisRepeaterStorage(redis *redis.Client) redisRepeaterStorage {
	return redisRepeaterStorage{
		Redis: redis,
	}
}

func (s *redisRepeaterStorage) ping(ctx context.Context, repeaterID uint) {
	repeater, err := s.get(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.LastPing = time.Now()
	s.store(ctx, repeaterID, repeater)
	s.Redis.Expire(ctx, fmt.Sprintf("repeater:%d", repeaterID), repeaterExpireTime)
}

func (s *redisRepeaterStorage) updateConnection(ctx context.Context, repeaterID uint, connection string) {
	repeater, err := s.get(ctx, repeaterID)
	if err != nil {
		klog.Errorf("Error getting repeater from redis", err)
		return
	}
	repeater.Connection = connection
	s.store(ctx, repeaterID, repeater)
}

func (s *redisRepeaterStorage) delete(ctx context.Context, repeaterID uint) bool {
	return s.Redis.Del(ctx, fmt.Sprintf("repeater:%d", repeaterID)).Val() == 1
}

func (s *redisRepeaterStorage) store(ctx context.Context, repeaterID uint, repeater models.Repeater) {
	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling repeater", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.Redis.Set(ctx, fmt.Sprintf("repeater:%d", repeaterID), repeaterBytes, repeaterExpireTime)
}

func (s *redisRepeaterStorage) get(ctx context.Context, repeaterID uint) (models.Repeater, error) {
	repeaterBits, err := s.Redis.Get(ctx, fmt.Sprintf("repeater:%d", repeaterID)).Result()
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

func (s *redisRepeaterStorage) exists(ctx context.Context, repeaterID uint) bool {
	return s.Redis.Exists(ctx, fmt.Sprintf("repeater:%d", repeaterID)).Val() == 1
}

func (s *redisRepeaterStorage) list(ctx context.Context) ([]uint, error) {
	var cursor uint64
	var repeaters []uint
	for {
		keys, _, err := s.Redis.Scan(ctx, cursor, "repeater:*", 0).Result()
		if err != nil {
			return nil, ErrNoSuchRepeater
		}
		for _, key := range keys {
			repeaterNum, err := strconv.Atoi(strings.Replace(key, "repeater:", "", 1))
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
