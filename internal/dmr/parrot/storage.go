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

package parrot

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/queue"
	"go.opentelemetry.io/otel"
)

type parrotStorage struct {
	kv    kv.KV
	queue *queue.Queue
}

var (
	ErrKV           = fmt.Errorf("KV error")
	ErrCast         = fmt.Errorf("cast error")
	ErrMarshal      = fmt.Errorf("marshal error")
	ErrUnmarshal    = fmt.Errorf("unmarshal error")
	ErrNoSuchStream = fmt.Errorf("no such stream")
)

const parrotExpireTime = 5 * time.Minute

func makeParrotStorage(kv kv.KV) parrotStorage {
	return parrotStorage{
		kv:    kv,
		queue: queue.NewQueue(),
	}
}

func (r *parrotStorage) store(ctx context.Context, streamID uint, repeaterID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.store")
	defer span.End()

	if err := r.kv.Set(ctx, fmt.Sprintf("parrot:stream:%d", streamID), []byte(strconv.FormatUint(uint64(repeaterID), 10))); err != nil {
		slog.Error("Error setting parrot stream", "streamID", streamID, "error", err)
	}
	if err := r.kv.Expire(ctx, fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime); err != nil {
		slog.Error("Error expiring parrot stream", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) exists(ctx context.Context, streamID uint) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.exists")
	defer span.End()

	has, err := r.kv.Has(ctx, fmt.Sprintf("parrot:stream:%d", streamID))
	if err != nil {
		slog.Error("Error checking if parrot stream exists", "streamID", streamID, "error", err)
		return false
	}
	return has
}

func (r *parrotStorage) refresh(ctx context.Context, streamID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.refresh")
	defer span.End()

	if err := r.kv.Expire(ctx, fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime); err != nil {
		slog.Error("Error refreshing parrot stream", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) get(ctx context.Context, streamID uint) (uint, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.get")
	defer span.End()

	repeaterIDStr, err := r.kv.Get(ctx, fmt.Sprintf("parrot:stream:%d", streamID))
	if err != nil {
		return 0, ErrKV
	}
	repeaterID, err := strconv.Atoi(string(repeaterIDStr))
	if err != nil {
		return 0, ErrCast
	}
	if repeaterID < 0 {
		return 0, ErrCast
	}
	return uint(repeaterID), nil
}

func (r *parrotStorage) stream(ctx context.Context, streamID uint, packet models.Packet) error {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.stream")
	defer span.End()

	packetBytes, err := packet.MarshalMsg(nil)
	if err != nil {
		return ErrMarshal
	}

	if _, err := r.queue.Push(fmt.Sprintf("parrot:stream:%d:packets", streamID), packetBytes); err != nil {
		slog.Error("Error pushing packet to parrot stream", "streamID", streamID, "error", err)
	}
	return nil
}

func (r *parrotStorage) delete(ctx context.Context, streamID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.delete")
	defer span.End()

	if err := r.kv.Delete(ctx, fmt.Sprintf("parrot:stream:%d", streamID)); err != nil {
		slog.Error("Error deleting parrot stream", "streamID", streamID, "error", err)
	}
	if err := r.queue.Delete(fmt.Sprintf("parrot:stream:%d:packets", streamID)); err != nil {
		slog.Error("Error deleting parrot stream packets", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) getStream(ctx context.Context, streamID uint) ([]models.Packet, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.getStream")
	defer span.End()

	packets := r.queue.Drain(fmt.Sprintf("parrot:stream:%d:packets", streamID))

	// Empty array of packets
	packetArray := make([]models.Packet, len(packets))
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
