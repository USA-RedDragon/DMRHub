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
	"log/slog"
	"strconv"

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
		peers, err := models.ListPeers(h.db)
		if err != nil {
			slog.Error("Failed to list peers for publishing", "error", err)
			return
		}
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
	encBuf := models.GetEncodeBuffer()
	var p models.RawDMRPacket
	p.Data = packet.EncodeTo(*encBuf)
	marshalBuf := h.getMarshalBuffer()
	packedBytes, err := p.MarshalMsg((*marshalBuf)[:0])
	models.PutEncodeBuffer(encBuf)
	if err != nil {
		h.putMarshalBuffer(marshalBuf)
		slog.Error("Error marshalling peer packet", "error", err)
		return
	}
	if err := h.pubsub.Publish("hub:packets:peers", packedBytes); err != nil {
		slog.Error("Error publishing packet to peers", "error", err)
	}
	h.putMarshalBuffer(marshalBuf)
}

// repeaterTopicPrefix is the fixed prefix for repeater pubsub topics.
const repeaterTopicPrefix = "hub:packets:repeater:"

// publishToRepeater publishes a packet to a specific repeater's pubsub topic.
func (h *Hub) publishToRepeater(repeaterID uint, packet models.Packet) {
	encBuf := models.GetEncodeBuffer()
	var rawPacket models.RawDMRPacket
	rawPacket.Data = packet.EncodeTo(*encBuf)
	marshalBuf := h.getMarshalBuffer()
	packedBytes, err := rawPacket.MarshalMsg((*marshalBuf)[:0])
	models.PutEncodeBuffer(encBuf)
	if err != nil {
		h.putMarshalBuffer(marshalBuf)
		slog.Error("Error marshalling packet for repeater", "error", err)
		return
	}
	// Build topic string without fmt.Sprintf allocation
	var topicBuf [len(repeaterTopicPrefix) + 20]byte
	n := copy(topicBuf[:], repeaterTopicPrefix)
	topic := string(topicBuf[:n]) + strconv.FormatUint(uint64(repeaterID), 10)
	if err := h.pubsub.Publish(topic, packedBytes); err != nil {
		slog.Error("Error publishing packet to repeater", "repeaterID", repeaterID, "error", err)
	}
	h.putMarshalBuffer(marshalBuf)
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
	buf := h.getBroadcastBuffer(totalLen)
	(*buf)[0] = byte(len(nameBytes))
	copy((*buf)[1:], nameBytes)
	packet.EncodeTo((*buf)[1+len(nameBytes):])
	msg := (*buf)[:totalLen]

	if err := h.pubsub.Publish("hub:packets:broadcast", msg); err != nil {
		slog.Error("Error publishing packet to broadcast", "error", err)
	}
	h.putBroadcastBuffer(buf)
}
