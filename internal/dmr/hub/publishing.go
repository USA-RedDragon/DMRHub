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
// used by publishToPeer, publishToRepeater, and handleGroupCallVoice.
func (h *Hub) marshalAndPublish(packet models.Packet, topic string) error {
	rawPacket := models.RawDMRPacket{
		Data: packet.Encode(),
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

// publishForPeers forwards a packet to all peer servers via pubsub.
// Queries the database for peers and checks egress rules.
// The context is used to abort the work early during shutdown.
func (h *Hub) publishForPeers(ctx context.Context, packet models.Packet) {
	if !h.hasPeerServers() {
		return
	}

	go func() {
		// Check for cancellation before doing DB work.
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
			// Re-check cancellation between iterations to avoid
			// unnecessary work when the hub is shutting down.
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
	}()
}

// publishToPeer publishes a packet to the peer delivery pubsub topic.
func (h *Hub) publishToPeer(peerID uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type for peer delivery", "signature", packet.Signature)
		return
	}

	// Set the repeater field to the peer ID for routing on the receiving end
	packet.Repeater = peerID
	if err := h.marshalAndPublish(packet, "hub:packets:peers"); err != nil {
		slog.Error("Error publishing packet to peers", "error", err)
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

// publishForBroadcastServers publishes a packet to the broadcast pubsub topic.
// sourceName is embedded so consumers can skip echo.
func (h *Hub) publishForBroadcastServers(packet models.Packet, sourceName string) {
	// Encode: [sourceNameLen(1 byte)][sourceName][packetData]
	nameBytes := []byte(sourceName)
	if len(nameBytes) > 255 {
		nameBytes = nameBytes[:255]
	}
	totalLen := 1 + len(nameBytes) + dmrconst.MMDVMMaxPacketLength
	buf := make([]byte, totalLen)
	buf[0] = byte(len(nameBytes))
	copy(buf[1:], nameBytes)
	packet.EncodeTo(buf[1+len(nameBytes):])
	msg := buf[:totalLen]

	if err := h.pubsub.Publish("hub:packets:broadcast", msg); err != nil {
		slog.Error("Error publishing packet to broadcast", "error", err)
	}
}
