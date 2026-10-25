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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

// trackCall queues the packet for the call tracker, which records every
// kind of transmission, and sets its TrackID.
func (h *Hub) trackCall(ctx context.Context, packet *models.Packet, sourceName string) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Hub.TrackCall")
	defer span.End()

	packet.TrackID = h.callTracker.Track(ctx, *packet, sourceName, h.getServerRole(sourceName) == RolePeer)
}

// RoutePacket is the central routing method for all incoming DMR packets.
// sourceName identifies the originating server (matches ServerConfig.Name)
// to prevent echo/loop back to the source. It queues the packet on the
// worker of its stream and returns at once, so the read loop of a server
// never waits on routing, and one stream never waits on another. A stream
// that another source is already sending is dropped (see AdmitStream). The
// call tracker gets the packet before the worker, in the order it came, so
// that a transmission that starts right after another on a slot does not
// overtake the end of that one.
func (h *Hub) RoutePacket(ctx context.Context, packet models.Packet, sourceName string) {
	if !h.AdmitSlotRate(packet, sourceName) || !h.AdmitStream(ctx, packet, sourceName) {
		return
	}
	h.trackCall(ctx, &packet, sourceName)
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
		defer h.endCodecStream(packet.StreamID)
	}

	h.noteHeardOn(packet, sourceName)
	defer h.noteSighting(packet, sourceName)

	sc := h.observeCodec(&packet, sourceName)

	if h.handleSpecialDestinations(ctx, packet, isVoice, sourceName) {
		return
	}

	if packet.GroupCall && isVoice && h.getServerRole(sourceName) != RolePeer {
		link := packet
		h.queueLink(ctx, packet.Repeater, func() { h.switchDynamicTalkgroup(ctx, link) })
	}

	tx := h.prepareCopy(ctx, &packet, sc, sourceName)
	h.fanOut(ctx, packet, sourceName)
	if tx != nil {
		tx.queue(packet)
	}
}

// fanOut sends packet, an original or a copy in the other codec, to the
// peers, the talkgroup or the private target.
func (h *Hub) fanOut(ctx context.Context, packet models.Packet, sourceName string) {
	_, isPlugin := plugins.PluginOfSource(sourceName)
	if !isPlugin && h.getServerRole(sourceName) != RolePeer {
		h.publishForPeers(ctx, packet)
	}

	if packet.GroupCall {
		if isPlugin {
			h.publishForPeers(ctx, packet)
		}
		h.handleGroupCall(packet)
		return
	}
	// A private call of a plugin goes to the peers only when the hub
	// cannot deliver it here, so that a reply to a radio on this hub,
	// such as the parrot's, does not reach it again through a peer.
	if !h.handlePrivateCall(ctx, packet) && isPlugin {
		h.publishForPeers(ctx, packet)
	}
}

// handleSpecialDestinations hands the calls to plugins' IDs to them, and
// handles unlink (TG 4000).
// Returns true if the packet was consumed (no further routing needed).
// Packets from network peers are not handled: their Repeater field is a
// peer ID, not a repeater to play back to or unlink.
func (h *Hub) handleSpecialDestinations(ctx context.Context, packet models.Packet, isVoice bool, sourceName string) bool {
	if h.toPlugin(packet, sourceName) {
		return true
	}
	if h.getServerRole(sourceName) == RolePeer {
		return false
	}
	if packet.Dst == 4000 && isVoice {
		h.queueLink(ctx, packet.Repeater, func() { h.doUnlink(ctx, packet) })
		return true
	}

	return false
}

// toPlugin hands packet to the plugin that owns its destination, unless the
// plugin sent it. A private call goes only to the plugin; a group call also
// goes to the talkgroup's subscribers when the talkgroup exists. It reports
// whether routing ends here.
func (h *Hub) toPlugin(packet models.Packet, sourceName string) bool {
	p := h.plugins.Load()
	if p == nil {
		return false
	}
	owner, ok := (*p).Owner(packet.Dst)
	if !ok {
		return false
	}
	if from, isPlugin := plugins.PluginOfSource(sourceName); !isPlugin || from != owner {
		(*p).Deliver(owner, packet)
	}
	if !packet.GroupCall {
		return true
	}
	if _, ok := h.streamTalkgroups.Load(streamTarget{stream: packet.StreamID, dst: packet.Dst}); ok {
		return false
	}
	exists, err := h.talkgroupIDs.Get(packet.Dst)
	if err != nil {
		slog.Error("Error checking if talkgroup exists", "error", err)
		return true
	}
	return !exists
}

// handleGroupCall routes a group burst of any kind to all subscribed
// repeaters.
func (h *Hub) handleGroupCall(packet models.Packet) {
	key := streamTarget{stream: packet.StreamID, dst: packet.Dst}
	if _, ok := h.streamTalkgroups.Load(key); !ok {
		exists, err := h.talkgroupIDs.Get(packet.Dst)
		if err != nil {
			slog.Error("Error checking if talkgroup exists", "error", err)
			return
		}
		if !exists {
			slog.Error("Talkgroup does not exist", "talkgroupID", packet.Dst)
			return
		}
		h.streamTalkgroups.Store(key, time.Now())
	}

	// talkgroupTopicPrefix is the fixed prefix for talkgroup pubsub topics.
	const talkgroupTopicPrefix = "hub:packets:talkgroup:"

	// Publish to talkgroup topic (subscription manager handles per-repeater fan-out)
	topic := talkgroupTopicPrefix + strconv.FormatUint(uint64(packet.Dst), 10)
	if err := h.marshalAndPublish(packet, topic); err != nil {
		slog.Error("Error publishing packet to talkgroup", "talkgroupID", packet.Dst, "error", err)
	}
}

// handlePrivateCall routes a private burst of any kind to the target
// repeater or user.
func (h *Hub) handlePrivateCall(ctx context.Context, packet models.Packet) bool {
	if isRepeaterID(packet.Dst) {
		return h.routeToRepeater(packet)
	}
	if isUserID(packet.Dst) {
		return h.routeToUser(ctx, packet)
	}
	return false
}

// routeToRepeater sends a private call packet to a specific repeater via
// pubsub. It reports whether the repeater exists.
func (h *Hub) routeToRepeater(packet models.Packet) bool {
	if _, err := h.routingRepeater(packet.Dst); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			slog.Error("Repeater does not exist", "repeaterID", packet.Dst)
		} else {
			slog.Error("Error checking if repeater exists", "error", err)
		}
		return false
	}

	// Publish to repeater topic — subscription manager delivers to the correct server
	h.publishToRepeater(packet.Dst, packet)
	return true
}

// routeToUser sends private traffic to the repeater where the registered
// radio was last heard. Traffic to a radio that is not a user is dropped.
// It reports whether the hub knows where the radio is.
func (h *Hub) routeToUser(ctx context.Context, packet models.Packet) bool {
	repeaterID, ok := h.privateTarget(ctx, packet)
	if !ok {
		return false
	}
	h.publishToRepeater(repeaterID, packet)
	return true
}

// privateTarget returns the repeater that the private stream of packet to
// a registered radio goes to: where its first burst went, or where the
// radio was last heard.
func (h *Hub) privateTarget(ctx context.Context, packet models.Packet) (uint, bool) {
	key := streamTarget{stream: packet.StreamID, dst: packet.Dst}
	if at, ok := h.streamTargets.Load(key); ok {
		return at.repeater, true
	}
	exists, err := h.userIDs.Get(packet.Dst)
	if err != nil {
		slog.Error("Error finding user", "error", err)
		return 0, false
	}
	if !exists {
		return 0, false
	}
	repeaterID, ok := h.locateRadio(ctx, packet.Dst)
	if !ok {
		slog.Debug("Dropping private traffic to a radio with no known location", "userID", packet.Dst, "src", packet.Src)
		return 0, false
	}
	h.streamTargets.Store(key, targetAt{repeater: repeaterID, at: time.Now()})
	return repeaterID, true
}

// privateTargetRepeater returns the repeater that the private burst
// packet goes to.
func (h *Hub) privateTargetRepeater(ctx context.Context, packet models.Packet) (*models.Repeater, bool) {
	id := packet.Dst
	if !isRepeaterID(id) {
		if !isUserID(id) {
			return nil, false
		}
		var ok bool
		if id, ok = h.privateTarget(ctx, packet); !ok {
			return nil, false
		}
	}
	rpt, err := h.routingRepeater(id)
	if err != nil {
		return nil, false
	}
	return rpt, true
}

// streamTarget is a private stream to a radio. The repeater that its first
// burst went to takes the rest of the stream, even when the radio is heard
// elsewhere meanwhile.
type streamTarget struct {
	stream uint
	dst    uint
}

type targetAt struct {
	repeater uint
	at       time.Time
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

// Plugins tells which plugin owns a DMR ID and hands it the bursts to that
// ID.
type Plugins interface {
	Owner(id uint) (uint, bool)
	Deliver(id uint, p models.Packet)
}

// SetPlugins makes p the plugins that the hub hands the bursts to their IDs.
func (h *Hub) SetPlugins(p Plugins) {
	h.plugins.Store(&p)
}
