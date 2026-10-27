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
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
)

// One replica, the holder of the egress lease in the KV store, sends all
// OpenBridge egress, so that peers see one source address. The others hand
// their egress to it over pubsub. A holder that stops in a handoff gives
// the lease up with a release on the egress topic; another replica takes
// it at once and says so with a taken message on the same topic. Since the
// topic has one order, the old holder sends each packet that comes before
// the taken message and the new holder each one that comes after it, so a
// packet is sent once and none is lost. When the holder dies, another
// replica takes the lease within EgressLeaseTTL plus one renewal; egress in
// that gap is dropped, never held.
const (
	egressLeaseKey = "dmrhub:openbridge:egress"
	// EgressLeaseTTL is the time after which the egress lease of a replica
	// that stopped renewing it expires.
	EgressLeaseTTL   = 2 * time.Second
	egressLeaseRenew = EgressLeaseTTL / 4
	egressTopic      = "openbridge:egress"

	quenchTopic  = "openbridge:quench"
	versionTopic = "openbridge:version"
	outboxSize   = 1024

	// takeWait is the longest time a replica that took the lease waits for
	// its taken message before it sends.
	takeWait = time.Second
	// handoverWait is the longest time a holder that gives the lease up
	// keeps sending while it waits for another replica to take it.
	handoverWait = 2 * time.Second
	// handoverCheck is how often a holder that gives the lease up looks
	// whether a replica took it without a taken message, as one of an
	// older DMRHub does.
	handoverCheck = 100 * time.Millisecond
)

// The roles of a replica in the egress lease.
const (
	roleFollower int32 = iota
	roleLeader
	// roleTaking is a replica that took the lease and waits for its taken
	// message before it sends.
	roleTaking
	// roleReleasing is a holder that gave the lease up and sends until the
	// taken message of another replica.
	roleReleasing
)

// The kinds of the messages on the egress topic besides packets.
const (
	kindRelease = "release"
	kindTaken   = "taken"
)

// outMessage is a message that an outbox publishes.
type outMessage struct {
	topic string
	msg   []byte
}

// egressJob is a packet that a replica without the lease hands to the
// holder, or, with a Kind, a message about the lease from Replica.
type egressJob struct {
	Peer     uint   `json:"peer"`
	Packet   []byte `json:"packet"`
	Server   uint32 `json:"server"`
	Repeater uint32 `json:"repeater"`
	Hops     uint8  `json:"hops"`
	Kind     string `json:"kind,omitempty"`
	Replica  string `json:"replica,omitempty"`
}

func newReplicaID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("openbridge-%d", time.Now().UnixNano())
	}
	return hex.EncodeToString(b[:])
}

// IsEgressLeader reports whether this replica holds the egress lease and
// sends.
func (s *Server) IsEgressLeader() bool {
	return s.role.Load() == roleLeader
}

// holdLease takes the egress lease when it is free and renews it when this
// replica holds it. Without a KV store, as one replica, it always holds it.
// When the KV store fails, a holder keeps the lease until it would have
// expired, as no other replica can take it before then.
func (s *Server) holdLease(ctx context.Context) bool {
	if s.kv == nil {
		return true
	}
	at := time.Now()
	held, err := s.tryLease(ctx)
	if err != nil {
		slog.Error("Error holding the OpenBridge egress lease", "error", err)
		return at.Before(s.leaseUntil)
	}
	if !held {
		s.leaseUntil = time.Time{}
		return false
	}
	s.leaseUntil = at.Add(EgressLeaseTTL)
	return true
}

// tryLease takes or renews the lease in the KV store and reports whether
// this replica holds it.
func (s *Server) tryLease(ctx context.Context) (bool, error) {
	ok, err := s.kv.SetNX(ctx, egressLeaseKey, s.id, EgressLeaseTTL)
	if err != nil {
		return false, fmt.Errorf("taking the lease: %w", err)
	}
	if ok {
		return true, nil
	}
	v, err := s.kv.Get(ctx, egressLeaseKey)
	if errors.Is(err, kv.ErrNotFound) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("reading the lease: %w", err)
	}
	if string(v) != s.id {
		return false, nil
	}
	if err := s.kv.Expire(ctx, egressLeaseKey, EgressLeaseTTL); err != nil {
		return false, fmt.Errorf("renewing the lease: %w", err)
	}
	return true, nil
}

func (s *Server) runLease(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(egressLeaseRenew)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		case <-s.released:
		}
		s.renewLease(ctx)
	}
}

// renewLease takes or renews the lease and moves the role of the replica
// on: a replica that takes the lease announces it and waits for its
// announcement before it sends, and one that lost the lease stops sending.
func (s *Server) renewLease(ctx context.Context) {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	role := s.role.Load()
	if role == roleReleasing {
		return
	}
	held := s.holdLease(ctx)
	switch {
	case held && role == roleFollower:
		s.beginTaking()
	case held && role == roleTaking && time.Since(s.takingAt) >= takeWait:
		s.setRole(roleLeader)
	case !held && role != roleFollower:
		s.setRole(roleFollower)
	}
}

// beginTaking says on the egress topic that this replica took the lease,
// and sends once that message comes back. Without pubsub, as one replica,
// it sends at once. The caller holds s.roleMu.
func (s *Server) beginTaking() {
	if s.pubsub == nil {
		s.setRole(roleLeader)
		return
	}
	s.takingAt = time.Now()
	s.setRole(roleTaking)
	if err := s.publishControl(kindTaken); err != nil {
		slog.Error("Error announcing the OpenBridge egress lease", "error", err)
		s.setRole(roleLeader)
	}
}

func (s *Server) setRole(role int32) {
	if old := s.role.Swap(role); old != role {
		slog.Info("OpenBridge egress lease", "held", role != roleFollower, "sends", role == roleLeader || role == roleReleasing, "replica", s.id)
	}
}

func (s *Server) publishControl(kind string) error {
	b, err := json.Marshal(egressJob{Kind: kind, Replica: s.id})
	if err != nil {
		return fmt.Errorf("encoding an OpenBridge lease message: %w", err)
	}
	if err := s.pubsub.Publish(egressTopic, b); err != nil {
		return fmt.Errorf("publishing an OpenBridge lease message: %w", err)
	}
	return nil
}

// sends reports whether this replica sends the packets on the egress
// topic as they come.
func (s *Server) sends() bool {
	role := s.role.Load()
	return role == roleLeader || role == roleReleasing
}

// releaseLease gives the lease up at a stop, so that another replica takes
// it at its next renewal.
func (s *Server) releaseLease(ctx context.Context) {
	if s.kv == nil {
		return
	}
	s.roleMu.Lock()
	role := s.role.Load()
	s.setRole(roleFollower)
	s.roleMu.Unlock()
	if role == roleLeader || role == roleTaking {
		s.deleteLease(ctx)
	}
}

func (s *Server) deleteLease(ctx context.Context) {
	if v, err := s.kv.Get(ctx, egressLeaseKey); err == nil && string(v) == s.id {
		if err := s.kv.Delete(ctx, egressLeaseKey); err != nil {
			slog.Error("Error giving up the OpenBridge egress lease", "error", err)
		}
	}
}

// handOver gives the lease of a holder that stops to another replica
// without a gap: it releases the lease and keeps sending the packets on
// the egress topic, its own too, until another replica says it took the
// lease, or handoverWait passes. A replica that holds the lease in the KV
// store sends only after its taken message, or after takeWait when the
// message does not come, as a replica of an older DMRHub sends no such
// message, so the holder keeps sending until then too: a packet then goes
// out twice rather than not at all.
func (s *Server) handOver(ctx context.Context) {
	s.roleMu.Lock()
	if s.kv == nil || s.pubsub == nil || s.role.Load() != roleLeader {
		s.roleMu.Unlock()
		return
	}
	s.setRole(roleReleasing)
	s.roleMu.Unlock()
	s.deleteLease(ctx)
	if err := s.publishControl(kindRelease); err != nil {
		slog.Error("Error handing the OpenBridge egress lease over", "error", err)
		return
	}
	check := time.NewTicker(handoverCheck)
	defer check.Stop()
	deadline := time.NewTimer(handoverWait)
	defer deadline.Stop()
	var taken <-chan time.Time
	for {
		select {
		case <-s.handedOver:
			return
		case <-taken:
			return
		case <-deadline.C:
			if taken != nil {
				continue
			}
			slog.Warn("No replica took the OpenBridge egress lease in time")
			return
		case <-check.C:
			if taken != nil {
				continue
			}
			if v, err := s.kv.Get(ctx, egressLeaseKey); err == nil && len(v) > 0 && string(v) != s.id {
				t := time.NewTimer(takeWait)
				defer t.Stop()
				taken = t.C
			}
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
	s.publishLater(s.egressOut, egressTopic, b)
}

// publishLater queues msg for the outbox out, so that no packet waits on
// pubsub. A full outbox drops msg.
func (s *Server) publishLater(out chan outMessage, topic string, msg []byte) {
	select {
	case out <- outMessage{topic: topic, msg: msg}:
	default:
		if n := s.outDropped.Add(1); n&(n-1) == 0 {
			slog.Warn("OpenBridge outbox full, messages dropped", "topic", topic, "dropped", n)
		}
	}
}

// runOutbox publishes the messages of out in order, and at the stop those
// that still wait, so that the replica that sends egress gets them.
func (s *Server) runOutbox(out chan outMessage) {
	defer s.wg.Done()
	publish := func(m outMessage) {
		if err := s.pubsub.Publish(m.topic, m.msg); err != nil {
			slog.Error("Error publishing an OpenBridge message", "topic", m.topic, "error", err)
		}
	}
	for {
		select {
		case <-s.done:
			for {
				select {
				case m := <-out:
					publish(m)
				default:
					return
				}
			}
		case m := <-out:
			publish(m)
		}
	}
}

func (s *Server) consumeEgress(sub pubsub.Subscription) {
	defer s.wg.Done()
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
			var j egressJob
			if err := json.Unmarshal(msg, &j); err != nil {
				slog.Error("Error decoding OpenBridge egress", "error", err)
				continue
			}
			if j.Kind != "" {
				s.leaseMessage(j)
				continue
			}
			if !s.sends() {
				continue
			}
			packet, ok := models.UnpackPacket(j.Packet)
			if !ok {
				continue
			}
			s.sendNow(j.Peer, packet, origin{server: j.Server, repeater: j.Repeater, hops: j.Hops})
		}
	}
}

// leaseMessage takes a release or taken message from the egress topic.
func (s *Server) leaseMessage(j egressJob) {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	role := s.role.Load()
	switch {
	case j.Kind == kindTaken && j.Replica == s.id && role == roleTaking:
		s.setRole(roleLeader)
	case j.Kind == kindTaken && j.Replica != s.id && role == roleReleasing:
		s.setRole(roleFollower)
		s.handedOverOnce.Do(func() { close(s.handedOver) })
	case j.Kind == kindTaken && j.Replica != s.id && role == roleLeader:
		s.setRole(roleFollower)
	case j.Kind == kindRelease && j.Replica != s.id && role == roleFollower:
		select {
		case s.released <- struct{}{}:
		default:
		}
	}
}

type sharedQuenchMsg struct {
	Peer   uint `json:"peer"`
	Stream uint `json:"stream"`
}

// shareQuench publishes a BCSQ to every replica, so that the replica that
// sends egress honors it whichever replica got it.
func (s *Server) shareQuench(q quench) {
	b, err := json.Marshal(sharedQuenchMsg{Peer: q.peer, Stream: q.stream})
	if err != nil {
		slog.Error("Error encoding an OpenBridge source quench", "error", err)
		return
	}
	s.publishLater(s.egressOut, quenchTopic, b)
}

// consumeQuenches takes the BCSQs that the replicas share.
func (s *Server) consumeQuenches() {
	defer s.wg.Done()
	sub := s.pubsub.Subscribe(quenchTopic)
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
			var m sharedQuenchMsg
			if err := json.Unmarshal(msg, &m); err != nil {
				slog.Error("Error decoding an OpenBridge source quench", "error", err)
				continue
			}
			s.quenched.Store(quench{peer: m.Peer, stream: m.Stream}, time.Now())
		}
	}
}

type sharedVersionMsg struct {
	Peer    uint  `json:"peer"`
	Version uint8 `json:"version"`
}

// noteVersion moves the link of peer up to version, which the peer named,
// on every replica, when DMRHub sends that version and the link is older.
func (s *Server) noteVersion(peer models.Peer, version uint8) {
	if s.linkVersion(peer) >= version || !s.raiseVersion(peer.ID, version) {
		return
	}
	slog.Info("OpenBridge peer moved its link to a newer version", "peerID", peer.ID, "version", version)
	if s.pubsub == nil {
		return
	}
	b, err := json.Marshal(sharedVersionMsg{Peer: peer.ID, Version: version})
	if err != nil {
		slog.Error("Error encoding an OpenBridge version", "error", err)
		return
	}
	s.publishLater(s.egressOut, versionTopic, b)
}

// raiseVersion records version for the link of peer and reports whether
// that moved the link up. Only versions 4 and 5 move it, as DMRHub sends
// no other DMRE.
func (s *Server) raiseVersion(peer uint, version uint8) bool {
	if s.versions == nil || (version != 4 && version != enhancedVersion) {
		return false
	}
	raised := false
	s.versions.Compute(peer, func(old uint8, loaded bool) (uint8, xsync.ComputeOp) {
		if loaded && old >= version {
			return old, xsync.CancelOp
		}
		raised = true
		return version, xsync.UpdateOp
	})
	return raised
}

// consumeVersions takes the link versions that the replicas share.
func (s *Server) consumeVersions() {
	defer s.wg.Done()
	sub := s.pubsub.Subscribe(versionTopic)
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
			var m sharedVersionMsg
			if err := json.Unmarshal(msg, &m); err != nil {
				slog.Error("Error decoding an OpenBridge version", "error", err)
				continue
			}
			s.raiseVersion(m.Peer, m.Version)
		}
	}
}
