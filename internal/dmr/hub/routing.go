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
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"go.opentelemetry.io/otel"
)

// trackCall centralizes call tracking for all protocols.
func (h *Hub) trackCall(ctx context.Context, packet models.Packet, isVoice bool) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.TrackCall")
	defer span.End()

	// Don't track unlink
	if packet.Dst == 4000 || !isVoice {
		return
	}

	if !h.callTracker.IsCallActive(ctx, packet) {
		h.callTracker.StartCall(ctx, packet)
	}
	if h.callTracker.IsCallActive(ctx, packet) {
		h.callTracker.ProcessCallPacket(ctx, packet)
		if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
			h.callTracker.EndCall(ctx, packet)
		}
	}
}

// RoutePacket is the central routing method for all incoming DMR packets.
// sourceName identifies the originating server (matches ServerConfig.Name)
// to prevent echo/loop back to the source.
func (h *Hub) RoutePacket(ctx context.Context, packet models.Packet, sourceName string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.RoutePacket")
	defer span.End()

	isVoice, isData := utils.CheckPacketType(packet)

	h.trackCall(ctx, packet, isVoice)

	// Handle special destinations first (parrot, unlink)
	if h.handleSpecialDestinations(ctx, packet, isVoice) {
		return
	}

	// Handle dynamic talkgroup linking for group voice calls
	if packet.GroupCall && isVoice {
		h.switchDynamicTalkgroup(ctx, packet)
	}

	// Forward to peer servers (skip if source is a peer server — it handles its own peer forwarding)
	if h.getServerRole(sourceName) != RolePeer {
		h.publishForPeers(ctx, packet)
	}

	// Route to repeater servers
	h.routePacket(ctx, packet, isVoice, isData, sourceName)
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

// routePacket routes a packet to repeater and broadcast servers.
func (h *Hub) routePacket(_ context.Context, packet models.Packet, isVoice, isData bool, sourceName string) {
	switch {
	case packet.GroupCall && isVoice:
		h.handleGroupCallVoice(packet, sourceName)
	case !packet.GroupCall && isVoice:
		h.handlePrivateCallVoice(packet)
	case isData:
		slog.Debug("Unhandled data packet type in hub")
	default:
		slog.Debug("Unhandled packet type in hub")
	}
}

// handleGroupCallVoice routes a group voice call to all subscribed repeaters.
func (h *Hub) handleGroupCallVoice(packet models.Packet, sourceName string) {
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

// handlePrivateCallVoice routes a private voice call to the target repeater or user.
func (h *Hub) handlePrivateCallVoice(packet models.Packet) {
	if isRepeaterID(packet.Dst) {
		h.routeToRepeater(packet)
	} else if isUserID(packet.Dst) {
		h.routeToUser(packet)
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

// routeToUser sends a private call to a user's repeaters via pubsub.
func (h *Hub) routeToUser(packet models.Packet) {
	userExists, err := models.UserIDExists(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error checking if user exists", "error", err)
		return
	}
	if !userExists {
		slog.Error("User does not exist", "userID", packet.Dst)
		return
	}

	user, err := models.FindUserByID(h.db, packet.Dst)
	if err != nil {
		slog.Error("Error finding user", "error", err)
		return
	}

	// Find user's last call to determine their most recent repeater
	var lastCall models.Call
	err = h.db.Where("user_id = ?", user.ID).Order("created_at DESC").First(&lastCall).Error
	switch {
	case err != nil:
		slog.Error("Error querying last call for user", "userID", user.ID, "error", err)
	case lastCall.ID != 0:
		h.publishToRepeater(lastCall.RepeaterID, packet)
	default:
		slog.Warn("Dropping private call to user: no previous call found to determine repeater", "userID", user.ID, "src", packet.Src)
	}
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
