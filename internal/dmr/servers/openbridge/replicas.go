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

package openbridge

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// One replica, the holder of the egress lease in the KV store, sends all
// OpenBridge egress, so that peers see one source address. The others hand
// their egress to it over pubsub. When the holder stops or dies, another
// replica takes the lease within EgressLeaseTTL plus one renewal; egress in
// that gap is dropped, never held.
const (
	egressLeaseKey = "dmrhub:openbridge:egress"
	// EgressLeaseTTL is the time after which the egress lease of a replica
	// that stopped renewing it expires.
	EgressLeaseTTL   = 2 * time.Second
	egressLeaseRenew = EgressLeaseTTL / 4
	egressTopic      = "openbridge:egress"

	quenchKeyPrefix = "dmrhub:openbridge:quench:"
	quenchRecheck   = time.Second
)

// egressJob is a packet that a replica without the lease hands to the
// holder.
type egressJob struct {
	Peer     uint   `json:"peer"`
	Packet   []byte `json:"packet"`
	Server   uint32 `json:"server"`
	Repeater uint32 `json:"repeater"`
	Hops     uint8  `json:"hops"`
}

func newReplicaID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("openbridge-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// IsEgressLeader reports whether this replica holds the egress lease.
func (s *Server) IsEgressLeader() bool {
	return s.leader.Load()
}

// holdLease takes the egress lease when it is free and renews it when this
// replica holds it. Without a KV store, as one replica, it always holds it.
func (s *Server) holdLease(ctx context.Context) bool {
	if s.kv == nil {
		return true
	}
	ok, err := s.kv.SetNX(ctx, egressLeaseKey, s.id, EgressLeaseTTL)
	if err != nil {
		slog.Error("Error taking the OpenBridge egress lease", "error", err)
		return false
	}
	if ok {
		return true
	}
	v, err := s.kv.Get(ctx, egressLeaseKey)
	if err != nil || string(v) != s.id {
		return false
	}
	if err := s.kv.Expire(ctx, egressLeaseKey, EgressLeaseTTL); err != nil {
		slog.Error("Error renewing the OpenBridge egress lease", "error", err)
		return false
	}
	return true
}

func (s *Server) runLease(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(egressLeaseRenew)
	defer ticker.Stop()
	for {
		was := s.leader.Load()
		now := s.holdLease(ctx)
		s.leader.Store(now)
		if now != was {
			slog.Info("OpenBridge egress lease", "held", now, "replica", s.id)
		}
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
	}
}

// releaseLease gives the lease up at a stop, so that another replica takes
// it at its next renewal.
func (s *Server) releaseLease(ctx context.Context) {
	if s.kv == nil || !s.leader.Load() {
		return
	}
	s.leader.Store(false)
	if v, err := s.kv.Get(ctx, egressLeaseKey); err == nil && string(v) == s.id {
		if err := s.kv.Delete(ctx, egressLeaseKey); err != nil {
			slog.Error("Error giving up the OpenBridge egress lease", "error", err)
		}
	}
}

// handOff publishes a packet for the replica that holds the lease.
func (s *Server) handOff(peerID uint, packet models.Packet, org origin) {
	b, err := json.Marshal(egressJob{Peer: peerID, Packet: packet.Encode(), Server: org.server, Repeater: org.repeater, Hops: org.hops})
	if err != nil {
		slog.Error("Error encoding OpenBridge egress", "error", err)
		return
	}
	if err := s.pubsub.Publish(egressTopic, b); err != nil {
		slog.Error("Error handing OpenBridge egress to the lease holder", "error", err)
	}
}

func (s *Server) consumeEgress(ctx context.Context) {
	defer s.wg.Done()
	sub := s.pubsub.Subscribe(egressTopic)
	defer func() { _ = sub.Close() }()
	ch := sub.Channel()
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			if !s.leader.Load() {
				continue
			}
			var j egressJob
			if err := json.Unmarshal(msg, &j); err != nil {
				slog.Error("Error decoding OpenBridge egress", "error", err)
				continue
			}
			packet, ok := models.UnpackPacket(j.Packet)
			if !ok {
				continue
			}
			s.sendNow(ctx, j.Peer, packet, origin{server: j.Server, repeater: j.Repeater, hops: j.Hops})
		}
	}
}

func quenchKey(q quench) string {
	return fmt.Sprintf("%s%d:%d", quenchKeyPrefix, q.peer, q.stream)
}

// shareQuench writes a BCSQ to the KV store, so that the replica that
// sends egress honors it whichever replica got it.
func (s *Server) shareQuench(ctx context.Context, q quench) {
	if s.kv == nil {
		return
	}
	if err := s.kv.Set(ctx, quenchKey(q), []byte{1}); err != nil {
		slog.Error("Error sharing an OpenBridge source quench", "error", err)
		return
	}
	if err := s.kv.Expire(ctx, quenchKey(q), quenchTTL); err != nil {
		slog.Error("Error setting the time of an OpenBridge source quench", "error", err)
	}
}

// sharedQuench reports whether another replica stored a BCSQ for q. It
// asks the KV store at most once a second for each stream.
func (s *Server) sharedQuench(ctx context.Context, q quench) bool {
	if s.kv == nil {
		return false
	}
	if at, ok := s.quenchChecked.Load(q); ok && time.Since(at) < quenchRecheck {
		return false
	}
	s.quenchChecked.Store(q, time.Now())
	ok, err := s.kv.Has(ctx, quenchKey(q))
	if err != nil || !ok {
		return false
	}
	s.quenched.Store(q, time.Now())
	return true
}
