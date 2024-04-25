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

package parrot

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
)

type redisParrotStorage struct {
	Redis *redis.Client
}

var (
	ErrRedis        = fmt.Errorf("redis error")
	ErrCast         = fmt.Errorf("cast error")
	ErrMarshal      = fmt.Errorf("marshal error")
	ErrUnmarshal    = fmt.Errorf("unmarshal error")
	ErrNoSuchStream = fmt.Errorf("no such stream")
)

const parrotExpireTime = 5 * time.Minute

func makeRedisParrotStorage(redis *redis.Client) redisParrotStorage {
	return redisParrotStorage{
		Redis: redis,
	}
}

func (r *redisParrotStorage) store(ctx context.Context, streamID uint, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.store")
	defer span.End()

	r.Redis.Set(ctx, fmt.Sprintf("parrot:stream:%d", streamID), repeaterID, parrotExpireTime)
}

func (r *redisParrotStorage) exists(ctx context.Context, streamID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.exists")
	defer span.End()

	return r.Redis.Exists(ctx, fmt.Sprintf("parrot:stream:%d", streamID)).Val() == 1
}

func (r *redisParrotStorage) refresh(ctx context.Context, streamID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.refresh")
	defer span.End()

	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime)
}

func (r *redisParrotStorage) get(ctx context.Context, streamID uint) (uint, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.get")
	defer span.End()

	repeaterIDStr, err := r.Redis.Get(ctx, fmt.Sprintf("parrot:stream:%d", streamID)).Result()
	if err != nil {
		return 0, ErrRedis
	}
	repeaterID, err := strconv.Atoi(repeaterIDStr)
	if err != nil {
		return 0, ErrCast
	}
	return uint(repeaterID), nil
}

func (r *redisParrotStorage) stream(ctx context.Context, streamID uint, packet models.Packet) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.stream")
	defer span.End()

	packetBytes, err := packet.MarshalMsg(nil)
	if err != nil {
		return ErrMarshal
	}

	r.Redis.RPush(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), packetBytes)
	return nil
}

func (r *redisParrotStorage) delete(ctx context.Context, streamID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.delete")
	defer span.End()

	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d", streamID))
	r.Redis.Expire(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), parrotExpireTime)
}

func (r *redisParrotStorage) getStream(ctx context.Context, streamID uint) ([]models.Packet, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "redisParrotStorage.getStream")
	defer span.End()

	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := r.Redis.LLen(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID)).Result()
	if err != nil {
		return nil, ErrNoSuchStream
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := r.Redis.LIndex(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID), i).Bytes()
		if err != nil {
			return nil, ErrNoSuchStream
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	r.Redis.Del(ctx, fmt.Sprintf("parrot:stream:%d:packets", streamID))

	// Empty array of packets
	packetArray := make([]models.Packet, packetSize)
	// Loop through the packets and unmarshal them
	for i, packet := range packets {
		var packetObj models.Packet
		_, err := packetObj.UnmarshalMsg(packet)
		if err != nil {
			return nil, ErrUnmarshal
		}
		packetArray[i] = packetObj
	}
	return packetArray, nil
}
