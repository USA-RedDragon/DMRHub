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
	"time"

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
		Codec:   packet.Codec,
		Copy:    packet.Copy,
		HasCopy: packet.HasCopy,
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

	set, ok := h.peerSet(ctx, packet)
	if !ok {
		return
	}
	for _, p := range set.peers {
		if !wantsPeerVersion(&packet, p.codec2) {
			continue
		}
		h.publishToPeer(p.id, packet)
		h.noteCopy(&packet, "peer:"+strconv.FormatUint(uint64(p.id), 10))
	}
}

type streamPeerSet struct {
	peers []peerTarget
	at    time.Time
}

type peerTarget struct {
	id     uint
	codec2 bool
}

// peerSet returns the peers that the stream of packet goes to, as the
// first burst of the stream found them.
func (h *Hub) peerSet(ctx context.Context, packet models.Packet) (streamPeerSet, bool) {
	key := streamTarget{stream: packet.StreamID, dst: packet.Src}
	if set, ok := h.streamPeers.Load(key); ok {
		return set, true
	}
	peers, err := h.egressPeers(ctx, packet)
	if err != nil {
		slog.Error("Failed to list peers for publishing", "error", err)
		return streamPeerSet{}, false
	}
	set := streamPeerSet{peers: peers, at: time.Now()}
	h.streamPeers.Store(key, set)
	return set, true
}

type egressPeer struct {
	peerTarget
	rules []models.PeerRule
}

func (h *Hub) loadEgressPeers(struct{}) ([]egressPeer, error) {
	peers, err := models.ListPeers(h.db)
	if err != nil {
		return nil, fmt.Errorf("listing peers: %w", err)
	}
	var out []egressPeer
	for _, p := range peers {
		if !p.Egress {
			continue
		}
		egressRules, err := models.ListEgressRulesForPeer(h.db, p.ID)
		if err != nil {
			return nil, fmt.Errorf("listing the egress rules of peer %d: %w", p.ID, err)
		}
		out = append(out, egressPeer{peerTarget: peerTarget{id: p.ID, codec2: p.Codec2}, rules: egressRules})
	}
	return out, nil
}

func (h *Hub) egressPeers(ctx context.Context, packet models.Packet) ([]peerTarget, error) {
	if ctx.Err() != nil {
		return nil, fmt.Errorf("listing peers: %w", ctx.Err())
	}
	peers, err := h.egressTable.Get(struct{}{})
	if err != nil {
		return nil, fmt.Errorf("looking up the peers: %w", err)
	}
	var out []peerTarget
	for _, p := range peers {
		if rules.EgressAllows(p.rules, &packet) {
			out = append(out, p.peerTarget)
		}
	}
	return out, nil
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
