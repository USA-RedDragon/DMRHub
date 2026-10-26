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

package servers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"go.opentelemetry.io/otel"
)

type KVClient struct {
	kv kv.KV
}

var (
	ErrNoSuchRepeater    = errors.New("no such repeater")
	ErrUnmarshalRepeater = errors.New("unmarshal repeater")
	ErrCastRepeater      = errors.New("unable to cast repeater id")
)

const (
	repeaterExpireTime = 5 * time.Minute
	repeaterKeyPrefix  = "mmdvm:repeater:"
)

func repeaterKey(repeaterID uint) string {
	return fmt.Sprintf("%s%d", repeaterKeyPrefix, repeaterID)
}

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
		slog.Error("Error getting repeater from KV", "repeaterID", repeaterID, "error", err)
		return
	}
	repeater.LastPing = time.Now()
	s.StoreRepeater(ctx, repeaterID, repeater)
	if err := s.kv.Expire(ctx, repeaterKey(repeaterID), repeaterExpireTime); err != nil {
		slog.Error("Error expiring repeater", "repeaterID", repeaterID, "error", err)
	}
}

func (s *KVClient) UpdateRepeaterConnection(ctx context.Context, repeaterID uint, connection models.RepeaterState) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.updateRepeaterConnection")
	defer span.End()

	repeater, err := s.GetRepeater(ctx, repeaterID)
	if err != nil {
		slog.Error("Error getting repeater from KV", "repeaterID", repeaterID, "error", err)
		return
	}
	repeater.Connection = connection
	s.StoreRepeater(ctx, repeaterID, repeater)
}

func (s *KVClient) DeleteRepeater(ctx context.Context, repeaterID uint) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.deleteRepeater")
	defer span.End()

	return s.kv.Delete(ctx, repeaterKey(repeaterID)) == nil
}

func (s *KVClient) StoreRepeater(ctx context.Context, repeaterID uint, repeater models.Repeater) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.storeRepeater")
	defer span.End()

	repeaterBytes, err := repeater.MarshalMsg(nil)
	if err != nil {
		slog.Error("Error marshalling repeater", "repeaterID", repeaterID, "error", err)
		return
	}
	// Expire repeaters after 5 minutes, this function called often enough to keep them alive
	if err := s.kv.Set(ctx, repeaterKey(repeaterID), repeaterBytes); err != nil {
		slog.Error("Error setting repeater in KV", "repeaterID", repeaterID, "error", err)
		return
	}
	if err := s.kv.Expire(ctx, repeaterKey(repeaterID), repeaterExpireTime); err != nil {
		slog.Error("Error expiring repeater", "repeaterID", repeaterID, "error", err)
	}
}

// GetRepeater returns the KV record of the repeater repeaterID, or
// ErrNoSuchRepeater when it has none.
func (s *KVClient) GetRepeater(ctx context.Context, repeaterID uint) (models.Repeater, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.getRepeater")
	defer span.End()

	repeaterBits, err := s.kv.Get(ctx, repeaterKey(repeaterID))
	if errors.Is(err, kv.ErrNotFound) {
		return models.Repeater{}, ErrNoSuchRepeater
	}
	if err != nil {
		slog.Error("Error getting repeater from KV", "repeaterID", repeaterID, "error", err)
		return models.Repeater{}, fmt.Errorf("getting repeater %d from KV: %w", repeaterID, err)
	}
	var repeater models.Repeater
	_, err = repeater.UnmarshalMsg(repeaterBits)
	if err != nil {
		slog.Error("Error unmarshalling repeater", "repeaterID", repeaterID, "error", err)
		return models.Repeater{}, ErrUnmarshalRepeater
	}
	return repeater, nil
}

func (s *KVClient) RepeaterExists(ctx context.Context, repeaterID uint) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.repeaterExists")
	defer span.End()

	has, err := s.kv.Has(ctx, repeaterKey(repeaterID))
	if err != nil {
		slog.Error("Error checking if repeater exists in KV", "repeaterID", repeaterID, "error", err)
		return false
	}
	return has
}

func (s *KVClient) ListRepeaters(ctx context.Context) ([]uint, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "KVClient.listRepeaters")
	defer span.End()

	keys, err := scanAll(ctx, s.kv, repeaterKeyPrefix+"*")
	if err != nil {
		return nil, ErrNoSuchRepeater
	}
	var repeaters []uint
	for _, key := range keys {
		repeaterNum, err := strconv.Atoi(strings.Replace(key, repeaterKeyPrefix, "", 1))
		if err != nil {
			return nil, ErrCastRepeater
		}
		if repeaterNum < 0 {
			return nil, ErrCastRepeater
		}
		repeaters = append(repeaters, uint(repeaterNum))
	}
	return repeaters, nil
}

// scanPage is how many keys scanAll asks the KV store for at a time.
const scanPage = 1000

// scanAll returns every key of store that matches match. A Redis scan
// returns its keys in pages, and the keys that match may be on any of them.
func scanAll(ctx context.Context, store kv.KV, match string) ([]string, error) {
	var out []string
	var cursor uint64
	for {
		keys, next, err := store.Scan(ctx, cursor, match, scanPage)
		if err != nil {
			return nil, fmt.Errorf("scanning %s: %w", match, err)
		}
		out = append(out, keys...)
		if next == 0 {
			return out, nil
		}
		cursor = next
	}
}
