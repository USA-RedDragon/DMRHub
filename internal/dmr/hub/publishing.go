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
	"fmt"
	"log/slog"
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
)

// marshalAndPublish encodes a packet, marshals it as a RawDMRPacket, and publishes
// to the given pubsub topic. This is the shared encode → marshal → publish pipeline
// used by publishToRepeater and handleGroupCall.
func (h *Hub) marshalAndPublish(packet models.Packet, topic string) error {
	rawPacket := models.RawDMRPacket{
		Data:    packet.Encode(),
		TrackID: packet.TrackID,
	}
	packedBytes, err := rawPacket.MarshalMsg(nil)
	if err != nil {
		return fmt.Errorf("marshaling RawDMRPacket: %w", err)
	}
	err = h.pubsub.Publish(topic, packedBytes)
	if err != nil {
		return fmt.Errorf("publishing to %s: %w", topic, err)
	}
	return nil
}

// publishForPeers queues a packet for egress to all peer servers.
// Packets of one stream run in order, so peers get them in the order the hub
// routed them. The context is used to abort the work early during shutdown.
func (h *Hub) publishForPeers(ctx context.Context, packet models.Packet) {
	if !h.hasPeerServers() {
		return
	}

	if !h.peerEgress.Do(ctx, packet.StreamID, func() { h.egressToPeers(ctx, packet) }) {
		slog.Warn("Dropping peer egress of a backed-up stream", "stream", packet.StreamID)
	}
}

func (h *Hub) egressToPeers(ctx context.Context, packet models.Packet) {
	select {
	case <-ctx.Done():
		return
	default:
	}

	peers, err := models.ListPeers(h.db)
	if err != nil {
		slog.Error("Failed to list peers for publishing", "error", err)
		return
	}
	for _, p := range peers {
		select {
		case <-ctx.Done():
			return
		default:
		}
		should, err := rules.PeerShouldEgress(h.db, p, &packet)
		if err != nil {
			slog.Error("Failed to check peer egress rules", "peerID", p.ID, "error", err)
			continue
		}
		if should {
			h.publishToPeer(p.ID, packet)
		}
	}
}

// publishToPeer hands a packet to the local peer servers for one peer. The
// replica that routes a packet sends it to the peers, so each peer gets it
// once whatever the number of replicas. The packet keeps the repeater that
// it came from, which an enhanced OpenBridge peer gets as the source
// repeater; RoutedPacket.RepeaterID names the peer.
func (h *Hub) publishToPeer(peerID uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type for peer delivery", "signature", packet.Signature)
		return
	}

	for _, name := range h.peerServers() {
		h.deliverToServer(name, RoutedPacket{RepeaterID: peerID, Packet: packet})
	}
}

// repeaterTopicPrefix is the fixed prefix for repeater pubsub topics.
const repeaterTopicPrefix = "hub:packets:repeater:"

// publishToRepeater publishes a packet to a specific repeater's pubsub topic.
func (h *Hub) publishToRepeater(repeaterID uint, packet models.Packet) {
	// Build topic string without fmt.Sprintf allocation
	var topicBuf [len(repeaterTopicPrefix) + 20]byte
	n := copy(topicBuf[:], repeaterTopicPrefix)
	topic := string(topicBuf[:n]) + strconv.FormatUint(uint64(repeaterID), 10)
	if err := h.marshalAndPublish(packet, topic); err != nil {
		slog.Error("Error publishing packet to repeater", "repeaterID", repeaterID, "error", err)
	}
}
