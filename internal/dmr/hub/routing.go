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
	"errors"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

// trackCall queues the packet for the call tracker, which records every
// kind of transmission.
func (h *Hub) trackCall(ctx context.Context, packet models.Packet, sourceName string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.TrackCall")
	defer span.End()

	h.callTracker.Track(ctx, packet, sourceName, h.getServerRole(sourceName) == RolePeer)
}

// RoutePacket is the central routing method for all incoming DMR packets.
// sourceName identifies the originating server (matches ServerConfig.Name)
// to prevent echo/loop back to the source. It queues the packet on the
// worker of its stream and returns at once, so the read loop of a server
// never waits on routing, and one stream never waits on another.
func (h *Hub) RoutePacket(ctx context.Context, packet models.Packet, sourceName string) {
	if !h.routing.Do(ctx, packet.StreamID, func() { h.routeNow(ctx, packet, sourceName) }) && ctx.Err() == nil {
		slog.Warn("Dropping packet of a backed-up stream", "stream", packet.StreamID, "src", packet.Src, "dst", packet.Dst)
	}
}

func (h *Hub) routeNow(ctx context.Context, packet models.Packet, sourceName string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.RoutePacket")
	defer span.End()

	isVoice, _ := utils.CheckPacketType(packet)

	h.streamActive(packet.StreamID)
	if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
		defer h.streamEnded(packet.StreamID)
	}

	h.trackCall(ctx, packet, sourceName)
	defer h.noteSighting(packet, sourceName)

	if h.handleSpecialDestinations(ctx, packet, isVoice) {
		return
	}

	if packet.GroupCall && isVoice {
		h.switchDynamicTalkgroup(ctx, packet)
	}

	if h.getServerRole(sourceName) != RolePeer {
		h.publishForPeers(ctx, packet)
	}

	if packet.GroupCall {
		h.handleGroupCall(packet, sourceName)
	} else {
		h.handlePrivateCall(ctx, packet)
	}
}

// handleSpecialDestinations handles parrot (TG 9990) and unlink (TG 4000).
// Returns true if the packet was consumed (no further routing needed).
func (h *Hub) handleSpecialDestinations(ctx context.Context, packet models.Packet, isVoice bool) bool {
	if packet.Dst == dmrconst.ParrotUser && isVoice {
		h.doParrot(ctx, packet)
		return true
	}

	if packet.Dst == 4000 && isVoice {
		h.doUnlink(ctx, packet)
		return true
	}

	return false
}

// handleGroupCall routes a group burst of any kind to all subscribed
// repeaters.
func (h *Hub) handleGroupCall(packet models.Packet, sourceName string) {
	exists, err := models.TalkgroupIDExists(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error checking if talkgroup exists", "error", err)
		return
	}
	if !exists {
		slog.Error("Talkgroup does not exist", "talkgroupID", packet.Dst)
		return
	}

	// talkgroupTopicPrefix is the fixed prefix for talkgroup pubsub topics.
	const talkgroupTopicPrefix = "hub:packets:talkgroup:"

	// Publish to talkgroup topic (subscription manager handles per-repeater fan-out)
	topic := talkgroupTopicPrefix + strconv.FormatUint(uint64(packet.Dst), 10)
	if err := h.marshalAndPublish(packet, topic); err != nil {
		slog.Error("Error publishing packet to talkgroup", "talkgroupID", packet.Dst, "error", err)
	}

	// Forward to broadcast servers (they handle echo filtering by source name)
	h.publishForBroadcastServers(packet, sourceName)
}

// handlePrivateCall routes a private burst of any kind to the target
// repeater or user.
func (h *Hub) handlePrivateCall(ctx context.Context, packet models.Packet) {
	if isRepeaterID(packet.Dst) {
		h.routeToRepeater(packet)
	} else if isUserID(packet.Dst) {
		h.routeToUser(ctx, packet)
	}
}

// routeToRepeater sends a private call packet to a specific repeater via pubsub.
func (h *Hub) routeToRepeater(packet models.Packet) {
	exists, err := models.RepeaterIDExists(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error checking if repeater exists", "error", err)
		return
	}
	if !exists {
		slog.Error("Repeater does not exist", "repeaterID", packet.Dst)
		return
	}

	// Publish to repeater topic — subscription manager delivers to the correct server
	h.publishToRepeater(packet.Dst, packet)
}

// routeToUser sends private traffic to the repeater where the registered
// radio was last heard. Traffic to a radio that is not a user is dropped.
func (h *Hub) routeToUser(ctx context.Context, packet models.Packet) {
	user, err := models.FindUserByID(h.db, packet.Dst)
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("Error finding user", "error", err)
		}
		return
	}
	repeaterID, ok := h.locateRadio(ctx, user.ID)
	if !ok {
		slog.Debug("Dropping private traffic to a radio with no known location", "userID", user.ID, "src", packet.Src)
		return
	}
	h.publishToRepeater(repeaterID, packet)
}

// isRepeaterID checks if an ID looks like a repeater (6-digit or 9-digit)
func isRepeaterID(id uint) bool {
	const (
		rptIDMin     = 100000
		rptIDMax     = 999999
		hotspotIDMin = 100000000
		hotspotIDMax = 999999999
	)
	return (id >= rptIDMin && id <= rptIDMax) || (id >= hotspotIDMin && id <= hotspotIDMax)
}

// isUserID checks if an ID looks like a user (7 or 8-digit)
func isUserID(id uint) bool {
	const (
		userIDMin = 1000000
		userIDMax = 99999999
	)
	return id >= userIDMin && id <= userIDMax
}
