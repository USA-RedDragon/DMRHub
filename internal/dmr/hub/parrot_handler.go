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

package hub

import (
	"context"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"go.opentelemetry.io/otel"
)

const parrotDelay = 3 * time.Second

// doParrot handles parrot (echo) service. Records packets and plays them back
// to the source repeater.
func (h *Hub) doParrot(ctx context.Context, packet models.Packet) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.doParrot")
	defer span.End()

	repeaterID := packet.Repeater
	if !h.parrot.IsStarted(ctx, packet.StreamID) {
		h.parrot.StartStream(ctx, packet.StreamID, repeaterID)
		slog.Debug("Parrot started for stream", "streamID", packet.StreamID, "repeaterID", repeaterID)
	}
	h.parrot.RecordPacket(ctx, packet.StreamID, packet)

	if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
		packets := h.parrot.GetStream(ctx, packet.StreamID)
		h.parrot.StopStream(ctx, packet.StreamID)
		go h.playbackParrot(ctx, repeaterID, packets)
	}
}

// playbackParrot plays back recorded parrot packets to the source repeater.
// Publishes to the repeater's pubsub topic so the subscription manager on
// whichever replica owns the repeater delivers it locally.
func (h *Hub) playbackParrot(ctx context.Context, repeaterID uint, packets []models.Packet) {
	time.Sleep(parrotDelay)

	startedTime := time.Now()
	for _, pkt := range packets {
		h.publishToRepeater(repeaterID, pkt)
		h.trackCall(ctx, pkt, true)

		elapsed := time.Since(startedTime)
		const packetTiming = 60 * time.Millisecond
		if elapsed > packetTiming {
			slog.Error("Parrot call took too long to send", "elapsed", elapsed)
			time.Sleep(packetTiming - (elapsed - packetTiming))
		} else {
			delay := packetTiming - elapsed
			time.Sleep(delay)
		}
		startedTime = time.Now()
	}
}
