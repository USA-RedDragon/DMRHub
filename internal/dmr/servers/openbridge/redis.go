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

package openbridge

import (
	"context"
	"errors"
	"fmt"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"k8s.io/klog/v2"
)

type redisClient struct {
	Redis  *redis.Client
	Tracer trace.Tracer
}

var (
	errNoSuchPeer    = errors.New("no such peer")
	errUnmarshalPeer = errors.New("unmarshal peer")
)

func makeRedisClient(redis *redis.Client) redisClient {
	return redisClient{
		Redis:  redis,
		Tracer: otel.Tracer("openbridge-redis"),
	}
}

func (s *redisClient) getPeer(ctx context.Context, peerID uint) (models.Peer, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	peerBits, err := s.Redis.Get(ctx, fmt.Sprintf("openbridge:peer:%d", peerID)).Result()
	if err != nil {
		klog.Errorf("Error getting peer from redis", err)
		return models.Peer{}, errNoSuchPeer
	}
	var peer models.Peer
	_, err = peer.UnmarshalMsg([]byte(peerBits))
	if err != nil {
		klog.Errorf("Error unmarshalling peer", err)
		return models.Peer{}, errUnmarshalPeer
	}
	return peer, nil
}
