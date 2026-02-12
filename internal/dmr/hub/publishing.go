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
	"fmt"
	"log/slog"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
)

// publishForPeers forwards a packet to all peer servers via pubsub.
// Queries the database for peers and checks egress rules.
func (h *Hub) publishForPeers(packet models.Packet) {
	if !h.hasPeerServers() {
		return
	}

	go func() {
		peers := models.ListPeers(h.db)
		for _, p := range peers {
			if rules.PeerShouldEgress(h.db, p, &packet) {
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
	p := models.RawDMRPacket{
		Data: packet.Encode(),
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		slog.Error("Error marshalling peer packet", "error", err)
		return
	}
	if err := h.pubsub.Publish("hub:packets:peers", packedBytes); err != nil {
		slog.Error("Error publishing packet to peers", "error", err)
	}
}

// publishToRepeater publishes a packet to a specific repeater's pubsub topic.
func (h *Hub) publishToRepeater(repeaterID uint, packet models.Packet) {
	var rawPacket models.RawDMRPacket
	rawPacket.Data = packet.Encode()
	packedBytes, err := rawPacket.MarshalMsg(nil)
	if err != nil {
		slog.Error("Error marshalling packet for repeater", "error", err)
		return
	}
	if err := h.pubsub.Publish(fmt.Sprintf("hub:packets:repeater:%d", repeaterID), packedBytes); err != nil {
		slog.Error("Error publishing packet to repeater", "repeaterID", repeaterID, "error", err)
	}
}

// publishForBroadcastServers publishes a packet to the broadcast pubsub topic.
// sourceName is embedded so consumers can skip echo.
func (h *Hub) publishForBroadcastServers(packet models.Packet, sourceName string) {
	// Encode: [sourceNameLen(1 byte)][sourceName][packetData]
	encodedPacket := packet.Encode()
	nameBytes := []byte(sourceName)
	if len(nameBytes) > 255 {
		nameBytes = nameBytes[:255]
	}
	msg := make([]byte, 0, 1+len(nameBytes)+len(encodedPacket))
	msg = append(msg, byte(len(nameBytes)))
	msg = append(msg, nameBytes...)
	msg = append(msg, encodedPacket...)

	if err := h.pubsub.Publish("hub:packets:broadcast", msg); err != nil {
		slog.Error("Error publishing packet to broadcast", "error", err)
	}
}
