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

package servers

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"go.opentelemetry.io/otel"
)

type KVClient struct {
	kv kv.KV
}

var (
	ErrNoSuchRepeater    = errors.New("no such repeater")
	ErrUnmarshalRepeater = errors.New("unmarshal repeater")
	ErrCastRepeater      = errors.New("unable to cast repeater id")
	ErrNoSuchPeer        = errors.New("no such peer")
	ErrUnmarshalPeer     = errors.New("unmarshal peer")
)

const repeaterExpireTime = 5 * time.Minute

func MakeKVClient(kv kv.KV) *KVClient {
	return &KVClient{
		kv: kv,
	}
}

func (s *KVClient) UpdateRepeaterPing(ctx context.Context, repeaterID uint) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.updateRepeaterPing")
	defer span.End()

	repeater, err := s.GetRepeater(ctx, repeaterID)
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
		return
	}
	repeater.LastPing = time.Now()
	s.StoreRepeater(ctx, repeaterID, repeater)
	s.kv.Expire(fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterExpireTime)
}

func (s *KVClient) UpdateRepeaterConnection(ctx context.Context, repeaterID uint, connection string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.updateRepeaterConnection")
	defer span.End()

	repeater, err := s.GetRepeater(ctx, repeaterID)
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
		return
	}
	repeater.Connection = connection
	s.StoreRepeater(ctx, repeaterID, repeater)
}

func (s *KVClient) DeleteRepeater(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.deleteRepeater")
	defer span.End()

	return s.kv.Delete(fmt.Sprintf("hbrp:repeater:%d", repeaterID)) == nil
}

func (s *KVClient) StoreRepeater(ctx context.Context, repeaterID uint, repeater models.Repeater) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.storeRepeater")
	defer span.End()

	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		logging.Errorf("Error marshalling repeater: %v", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	s.kv.Set(fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterBytes)
	s.kv.Expire(fmt.Sprintf("hbrp:repeater:%d", repeaterID), repeaterExpireTime)
}

func (s *KVClient) GetRepeater(ctx context.Context, repeaterID uint) (models.Repeater, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.getRepeater")
	defer span.End()

	repeaterBits, err := s.kv.Get(fmt.Sprintf("hbrp:repeater:%d", repeaterID))
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
		return models.Repeater{}, ErrNoSuchRepeater
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg([]byte(repeaterBits))
	if err != nil {
		logging.Errorf("Error unmarshalling repeater: %v", err)
		return models.Repeater{}, ErrUnmarshalRepeater
	}
	return repeater, nil
}

func (s *KVClient) RepeaterExists(ctx context.Context, repeaterID uint) bool {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.repeaterExists")
	defer span.End()

	has, err := s.kv.Has(fmt.Sprintf("hbrp:repeater:%d", repeaterID))
	if err != nil {
		logging.Errorf("Error checking if repeater exists in KV: %v", err)
		return false
	}
	return has
}

func (s *KVClient) ListRepeaters(ctx context.Context) ([]uint, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.listRepeaters")
	defer span.End()

	var cursor uint64
	var repeaters []uint
	for {
		keys, _, err := s.kv.Scan(cursor, "hbrp:repeater:*", 0)
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

func (s *KVClient) GetPeer(ctx context.Context, peerID uint) (models.Peer, error) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.getPeer")
	defer span.End()

	peerBits, err := s.kv.Get(fmt.Sprintf("openbridge:peer:%d", peerID))
	if err != nil {
		logging.Errorf("Error getting peer from KV: %v", err)
		return models.Peer{}, ErrNoSuchPeer
	}
	var peer models.Peer
	_, err = peer.UnmarshalMsg([]byte(peerBits))
	if err != nil {
		logging.Errorf("Error unmarshalling peer: %v", err)
		return models.Peer{}, ErrUnmarshalPeer
	}
	return peer, nil
}
