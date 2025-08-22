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
	"log/slog"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"go.opentelemetry.io/otel"
)

type parrotStorage struct {
	kv kv.KV
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
		kv: kv,
	}
}

func (r *parrotStorage) store(ctx context.Context, streamID uint, repeaterID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.store")
	defer span.End()

	if err := r.kv.Set(fmt.Sprintf("parrot:stream:%d", streamID), []byte(strconv.Itoa(int(repeaterID)))); err != nil {
		slog.Error("Error setting parrot stream", "streamID", streamID, "error", err)
	}
	if err := r.kv.Expire(fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime); err != nil {
		slog.Error("Error expiring parrot stream", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) exists(ctx context.Context, streamID uint) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.exists")
	defer span.End()

	has, err := r.kv.Has(fmt.Sprintf("parrot:stream:%d", streamID))
	if err != nil {
		slog.Error("Error checking if parrot stream exists", "streamID", streamID, "error", err)
		return false
	}
	return has
}

func (r *parrotStorage) refresh(ctx context.Context, streamID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.refresh")
	defer span.End()

	if err := r.kv.Expire(fmt.Sprintf("parrot:stream:%d", streamID), parrotExpireTime); err != nil {
		slog.Error("Error refreshing parrot stream", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) get(ctx context.Context, streamID uint) (uint, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.get")
	defer span.End()

	repeaterIDStr, err := r.kv.Get(fmt.Sprintf("parrot:stream:%d", streamID))
	if err != nil {
		return 0, ErrKV
	}
	repeaterID, err := strconv.Atoi(string(repeaterIDStr))
	if err != nil {
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

	if _, err := r.kv.RPush(fmt.Sprintf("parrot:stream:%d:packets", streamID), packetBytes); err != nil {
		slog.Error("Error pushing packet to parrot stream", "streamID", streamID, "error", err)
	}
	return nil
}

func (r *parrotStorage) delete(ctx context.Context, streamID uint) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.delete")
	defer span.End()

	if err := r.kv.Delete(fmt.Sprintf("parrot:stream:%d", streamID)); err != nil {
		slog.Error("Error deleting parrot stream", "streamID", streamID, "error", err)
	}
	if err := r.kv.Expire(fmt.Sprintf("parrot:stream:%d:packets", streamID), parrotExpireTime); err != nil {
		slog.Error("Error expiring parrot stream packets", "streamID", streamID, "error", err)
	}
}

func (r *parrotStorage) getStream(ctx context.Context, streamID uint) ([]models.Packet, error) {
	_, span := otel.Tracer("DMRHub").Start(ctx, "kvParrotStorage.getStream")
	defer span.End()

	// Empty array of packet byte arrays
	var packets [][]byte
	packetSize, err := r.kv.LLen(fmt.Sprintf("parrot:stream:%d:packets", streamID))
	if err != nil {
		return nil, ErrNoSuchStream
	}
	// Loop through the packets and add them to the array
	for i := int64(0); i < packetSize; i++ {
		packet, err := r.kv.LIndex(fmt.Sprintf("parrot:stream:%d:packets", streamID), i)
		if err != nil {
			return nil, ErrNoSuchStream
		}
		packets = append(packets, packet)
	}
	// Delete the stream
	if err := r.kv.Delete(fmt.Sprintf("parrot:stream:%d:packets", streamID)); err != nil {
		slog.Error("Error deleting parrot stream packets", "streamID", streamID, "error", err)
	}

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
