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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
)

// marshalAndPublish encodes a packet, marshals it as a RawDMRPacket, and
// queues its publish to topic on the outbox of the source slot key, so that
// routing never waits on pubsub. The outbox of a source slot publishes in
// the order of the calls, so a target gets the bursts of each source slot
// in the order they were routed.
func (h *Hub) marshalAndPublish(key sourceSlot, packet models.Packet, topic string) {
	rawPacket := models.RawDMRPacket{
		Data:    packet.Encode(),
		TrackID: packet.TrackID,
		Codec:   packet.Codec,
		Copy:    packet.Copy,
		HasCopy: packet.HasCopy,
	}
	packedBytes, err := rawPacket.MarshalMsg(nil)
	if err != nil {
		slog.Error("Error marshaling a burst to publish", "topic", topic, "error", err)
		return
	}
	h.outbox.Add(key, outbox.Job{Topic: topic, Data: packedBytes})
}

// publishForPeers queues a packet from sourceName for egress to all peer
// servers. The packets of one source slot run in order, so peers get them
// in the order the hub routed them. The context is used to abort the work
// early during shutdown.
func (h *Hub) publishForPeers(ctx context.Context, packet models.Packet, sourceName string) {
	if !h.hasPeerServers() {
		return
	}

	if !h.peerEgress.Do(ctx, h.sourceSlotOf(packet, sourceName), func() { h.egressToPeers(ctx, packet) }) {
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
	rules   []models.PeerRule
	private bool
}

// loadPeers reads the peers that the hub sends bursts to from the
// database, so that the first burst after a start does not wait on it.
func (h *Hub) loadPeers() {
	peers, err := h.loadEgressPeers(struct{}{})
	if err != nil {
		slog.Error("Cannot load the peers", "error", err)
		return
	}
	h.egressTable.Put(struct{}{}, peers)
}

// refreshPeers reads the peers that the hub sends bursts to from the
// database again, once the hub read them, so that a quiet hub keeps them.
// When the database fails, the hub keeps the peers that it had.
func (h *Hub) refreshPeers() {
	if err := h.egressTable.Refresh(struct{}{}); err != nil {
		slog.Error("Cannot load the peers, keeping the ones known", "error", err)
	}
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
		out = append(out, egressPeer{peerTarget: peerTarget{id: p.ID, codec2: p.Codec2}, rules: egressRules, private: p.EgressPrivate})
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
		if rules.EgressAllows(p.rules, p.private, &packet) {
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

// publishToRepeater publishes a packet of the source slot key to a
// specific repeater's pubsub topic.
func (h *Hub) publishToRepeater(key sourceSlot, repeaterID uint, packet models.Packet) {
	// Build topic string without fmt.Sprintf allocation
	var topicBuf [len(repeaterTopicPrefix) + 20]byte
	n := copy(topicBuf[:], repeaterTopicPrefix)
	topic := string(topicBuf[:n]) + strconv.FormatUint(uint64(repeaterID), 10)
	h.marshalAndPublish(key, packet, topic)
}
