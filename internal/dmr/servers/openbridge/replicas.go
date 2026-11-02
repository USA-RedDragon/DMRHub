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
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
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
// packet is sent once and none is lost. A holder that stops says so in its
// release message and says on the topic once it read the taken message,
// and so sent its part; the new holder holds what comes after its taken
// message until then, for at most handoverWait, so that no peer gets a
// packet after a later one. A replica that held a packet in its outbox when
// it took the lease sends its own packets through the topic until it read
// back each one that it handed off before. When the holder dies, another
// replica takes the lease within EgressLeaseTTL plus one renewal; egress in
// that gap is dropped, never held.
const (
	egressLeaseKey = "dmrhub:openbridge:egress"
	// EgressLeaseTTL is the time after which the egress lease of a replica
	// that stopped renewing it expires.
	EgressLeaseTTL   = 2 * time.Second
	egressLeaseRenew = EgressLeaseTTL / 4
	egressTopic      = "openbridge:egress"

	quenchTopic      = "openbridge:quench"
	versionTopic     = "openbridge:version"
	versionKeyPrefix = "openbridge:version:"
	outboxSize       = 1024

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
	roleSettling
)

// The kinds of the messages on the egress topic besides packets.
const (
	kindRelease = "release"
	kindTaken   = "taken"
	kindMarker  = "marker"
	kindAck     = "ack"
	kindHanded  = "handed"
)

const leaseAcks = "acks"

func (s *Server) leaseValue() string {
	return s.id + " " + leaseAcks
}

// outMessage is a message that an outbox publishes.
type outMessage struct {
	topic string
	msg   []byte
	then  func()
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
	Marker   string `json:"marker,omitempty"`
	Seq      uint64 `json:"seq,omitempty"`
	Hands    bool   `json:"hands,omitempty"`
	To       string `json:"to,omitempty"`
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
	at := s.clock.Now()
	held, err := s.tryLease(ctx)
	s.takenElsewhere = errors.Is(err, errTakenElsewhere)
	if s.takenElsewhere {
		s.leaseUntil = time.Time{}
		return false
	}
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

// errTakenElsewhere is the answer of tryLease when another replica holds
// the lease.
var errTakenElsewhere = errors.New("another replica holds the lease")

// tryLease takes or renews the lease in the KV store and reports whether
// this replica holds it, or errTakenElsewhere.
func (s *Server) tryLease(ctx context.Context) (bool, error) {
	ok, err := s.kv.SetNX(ctx, egressLeaseKey, s.leaseValue(), EgressLeaseTTL)
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
	if string(v) != s.leaseValue() {
		return false, errTakenElsewhere
	}
	if err := s.kv.Expire(ctx, egressLeaseKey, EgressLeaseTTL); err != nil {
		return false, fmt.Errorf("renewing the lease: %w", err)
	}
	return true, nil
}

func (s *Server) runLease(ctx context.Context) {
	defer s.wg.Done()
	ticker := s.clock.NewTicker(egressLeaseRenew)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C():
		case <-s.released:
		}
		s.renewLease(ctx)
	}
}

// renewLease takes or renews the lease and moves the role of the replica
// on: a replica that takes the lease announces it and waits for its
// announcement before it sends, and a holder that lost the lease sends
// until the taken message of the replica that took it, or for takeWait,
// as that replica sends only the packets after its message.
// A stopped replica does nothing, so that a renewal that waited on the
// role lock does not take the lease that the stop gave up.
func (s *Server) renewLease(ctx context.Context) {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	select {
	case <-s.done:
		return
	default:
	}
	role := s.role.Load()
	if role == roleReleasing {
		return
	}
	held := s.holdLease(ctx)
	if held || role != roleLeader || !s.takenElsewhere {
		s.lostAt = time.Time{}
	}
	switch {
	case held && role == roleFollower:
		s.beginTaking()
	case held && role == roleTaking && s.clock.Since(s.takingAt) >= takeWait:
		s.setRole(roleLeader)
	case !held && role == roleLeader && s.takenElsewhere && s.lostAt.IsZero():
		s.lostAt = s.clock.Now()
	case !held && role == roleLeader && s.takenElsewhere && s.clock.Since(s.lostAt) < takeWait:
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
	s.takingAt = s.clock.Now()
	s.setRole(roleTaking)
	if err := s.publishControl(egressJob{Kind: kindTaken}); err != nil {
		slog.Error("Error announcing the OpenBridge egress lease", "error", err)
		s.setRole(roleLeader)
	}
}

func (s *Server) setRole(role int32) {
	if old := s.role.Swap(role); old != role {
		slog.Info("OpenBridge egress lease", "held", role != roleFollower, "sends", role == roleLeader || role == roleReleasing, "replica", s.id)
	}
}

func (s *Server) publishControl(j egressJob) error {
	j.Replica = s.id
	b, err := json.Marshal(j)
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
	s.setRole(roleFollower)
	s.roleMu.Unlock()
	s.deleteLease(ctx)
}

// deleteLease deletes the egress lease when this replica holds it in the
// KV store, whatever its role says, since a SetNX whose reply timed out may
// have taken it.
func (s *Server) deleteLease(ctx context.Context) {
	if _, err := s.kv.DeleteIfEqual(ctx, egressLeaseKey, []byte(s.leaseValue())); err != nil {
		slog.Error("Error giving up the OpenBridge egress lease", "error", err)
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
	if err := s.publishControl(egressJob{Kind: kindRelease, Hands: true}); err != nil {
		slog.Error("Error handing the OpenBridge egress lease over", "error", err)
		return
	}
	check := s.clock.NewTicker(handoverCheck)
	defer check.Stop()
	deadline := s.clock.NewTimer(handoverWait)
	defer deadline.Stop()
	var taken <-chan time.Time
	for {
		select {
		case <-s.handedOver:
			return
		case <-taken:
			return
		case <-deadline.C():
			if taken != nil {
				continue
			}
			slog.Warn("No replica took the OpenBridge egress lease in time")
			return
		case <-check.C():
			if taken != nil {
				continue
			}
			if v, err := s.kv.Get(ctx, egressLeaseKey); err == nil && len(v) > 0 && string(v) != s.leaseValue() {
				t := s.clock.NewTimer(takeWait)
				defer t.Stop()
				taken = t.C()
			}
		}
	}
}

// handOff publishes a packet for the replica that holds the lease.
func (s *Server) handOff(peerID uint, packet models.Packet, org origin) {
	s.ownMu.Lock()
	defer s.ownMu.Unlock()
	seq := s.ownHanded + 1
	b, err := json.Marshal(egressJob{Peer: peerID, Packet: packet.Encode(), Server: org.server, Repeater: org.repeater, Hops: org.hops, Replica: s.id, Seq: seq})
	if err != nil {
		slog.Error("Error encoding OpenBridge egress", "error", err)
		return
	}
	if s.publishLater(s.egressOut, egressTopic, b) {
		s.ownHanded = seq
	}
}

func (s *Server) handedBehind() bool {
	s.ownMu.Lock()
	defer s.ownMu.Unlock()
	return s.ownHanded > s.ownBack.Load()
}

// publishLater queues msg for the outbox out, so that no packet waits on
// pubsub, and reports whether it did. A full outbox drops msg.
func (s *Server) publishLater(out chan outMessage, topic string, msg []byte) bool {
	select {
	case out <- outMessage{topic: topic, msg: msg}:
		return true
	default:
		if n := s.outDropped.Add(1); n&(n-1) == 0 {
			slog.Warn("OpenBridge outbox full, messages dropped", "topic", topic, "dropped", n)
		}
		return false
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
		if m.then != nil {
			m.then()
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
	var settle clock.Timer
	defer func() {
		if settle != nil {
			settle.Stop()
		}
	}()
	for {
		var settled <-chan time.Time
		if settle != nil {
			settled = settle.C()
		}
		select {
		case <-s.done:
			return
		case <-settled:
			settle = nil
			s.endSettling()
		case msg, ok := <-ch:
			if !ok {
				return
			}
			s.egressMessage(msg)
		}
		switch settling := s.role.Load() == roleSettling; {
		case settling && settle == nil:
			settle = s.clock.NewTimer(handoverWait)
		case !settling && settle != nil:
			settle.Stop()
			settle = nil
		}
	}
}

func (s *Server) egressMessage(msg []byte) {
	var j egressJob
	if err := json.Unmarshal(msg, &j); err != nil {
		slog.Error("Error decoding OpenBridge egress", "error", err)
		return
	}
	if s.role.Load() == roleSettling && (j.Kind == "" || j.Kind == kindMarker) {
		s.held = append(s.held, j)
		return
	}
	s.egressJob(j, false)
	if len(s.held) > 0 && s.role.Load() != roleSettling {
		s.sendHeld()
	}
}

func (s *Server) egressJob(j egressJob, held bool) {
	if j.Kind == kindMarker && held {
		s.ack(j)
		return
	}
	if j.Kind != "" {
		s.controlMessage(j)
		return
	}
	if s.sends() || held {
		if packet, ok := models.UnpackPacket(j.Packet); ok {
			s.sendNow(j.Peer, packet, origin{server: j.Server, repeater: j.Repeater, hops: j.Hops})
		}
	}
	if j.Replica == s.id && j.Seq > s.ownBack.Load() {
		s.ownBack.Store(j.Seq)
	}
}

func (s *Server) sendHeld() {
	held := s.held
	s.held = nil
	for _, j := range held {
		s.egressJob(j, true)
	}
}

func (s *Server) endSettling() {
	s.roleMu.Lock()
	if s.role.Load() == roleSettling {
		s.setRole(roleLeader)
	}
	s.releasedBy = ""
	s.roleMu.Unlock()
	s.sendHeld()
}

func (s *Server) controlMessage(j egressJob) {
	switch j.Kind {
	case kindMarker:
		if s.sends() {
			s.ack(j)
		}
	case kindHanded:
		if j.To == s.id && j.Replica == s.releasedByNow() {
			s.endSettling()
		}
	case kindAck:
		if j.Replica != s.id {
			return
		}
		if done, ok := s.markers.LoadAndDelete(j.Marker); ok {
			close(done)
		}
	default:
		s.leaseMessage(j)
	}
}

func (s *Server) ack(m egressJob) {
	b, err := json.Marshal(egressJob{Kind: kindAck, Replica: m.Replica, Marker: m.Marker})
	if err != nil {
		slog.Error("Error encoding an OpenBridge ack", "error", err)
		return
	}
	if err := s.pubsub.Publish(egressTopic, b); err != nil {
		slog.Error("Error publishing an OpenBridge ack", "error", err)
	}
}

func (s *Server) settleEgress(ctx context.Context, done chan struct{}) {
	token := s.id + ":" + strconv.FormatUint(s.markerSeq.Add(1), 10)
	b, err := json.Marshal(egressJob{Kind: kindMarker, Replica: s.id, Marker: token})
	if err != nil {
		slog.Error("Error encoding an OpenBridge marker", "error", err)
		close(done)
		return
	}
	m := outMessage{topic: egressTopic, msg: b}
	if s.holderAcks(ctx) {
		s.markers.Store(token, done)
	} else {
		m.then = func() { close(done) }
	}
	select {
	case s.egressOut <- m:
	case <-s.done:
		if _, ok := s.markers.LoadAndDelete(token); ok || m.then != nil {
			close(done)
		}
	}
}

func (s *Server) holderAcks(ctx context.Context) bool {
	if s.kv == nil {
		return true
	}
	v, err := s.kv.Get(ctx, egressLeaseKey)
	if err != nil {
		return false
	}
	f := strings.Fields(string(v))
	return len(f) == 2 && f[1] == leaseAcks
}

func (s *Server) releasedByNow() string {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	return s.releasedBy
}

// leaseMessage takes a release or taken message from the egress topic.
func (s *Server) leaseMessage(j egressJob) {
	s.roleMu.Lock()
	defer s.roleMu.Unlock()
	role := s.role.Load()
	switch {
	case j.Kind == kindTaken && j.Replica == s.id && role == roleTaking && s.releasedBy != "":
		s.setRole(roleSettling)
	case j.Kind == kindTaken && j.Replica == s.id && role == roleTaking:
		s.setRole(roleLeader)
	case j.Kind == kindTaken && j.Replica != s.id && role == roleReleasing:
		s.setRole(roleFollower)
		s.handedOverOnce.Do(func() { close(s.handedOver) })
		if s.pubsub == nil {
			break
		}
		if err := s.publishControl(egressJob{Kind: kindHanded, To: j.Replica}); err != nil {
			slog.Error("Error saying that the OpenBridge egress lease was handed over", "error", err)
		}
	case j.Kind == kindTaken && j.Replica != s.id && (role == roleLeader || role == roleSettling):
		s.setRole(roleFollower)
	case j.Kind == kindRelease && j.Replica != s.id && role == roleFollower:
		select {
		case s.released <- struct{}{}:
		default:
		}
	}
	switch {
	case j.Kind == kindRelease && j.Replica != s.id && j.Hands:
		s.releasedBy = j.Replica
	case j.Kind == kindRelease, j.Kind == kindTaken && j.Replica != s.id:
		s.releasedBy = ""
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
			s.quenched.Store(quench{peer: m.Peer, stream: m.Stream}, s.clock.Now())
		}
	}
}

type sharedVersionMsg struct {
	Peer       uint  `json:"peer"`
	Version    uint8 `json:"version"`
	Generation int64 `json:"generation"`
}

// peerVersion is the version that a peer named for its link, and the
// generation of the settings of the peer when it named it.
type peerVersion struct {
	version    uint8
	generation int64
}

// generation names the settings of peer: every change of the peer moves
// its UpdatedAt, and a peer added again has a new one. A version of an older
// generation is of settings that changed since.
func generation(peer models.Peer) int64 {
	if peer.UpdatedAt.IsZero() {
		return 0
	}
	return peer.UpdatedAt.UnixNano()
}

// noteVersion moves the link of peer up to version, which the peer named,
// on every replica, when DMRHub sends that version and the link is older.
func (s *Server) noteVersion(ctx context.Context, peer models.Peer, version uint8) {
	gen := generation(peer)
	if s.linkVersion(peer) >= version || !s.raiseVersion(peer.ID, version, gen) {
		return
	}
	slog.Info("OpenBridge peer moved its link to a newer version", "peerID", peer.ID, "version", version)
	if s.kv != nil {
		if err := s.kv.Set(ctx, versionKey(peer.ID, gen), []byte{version}); err != nil {
			slog.Error("Error storing an OpenBridge version", "peerID", peer.ID, "error", err)
		}
	}
	if s.pubsub == nil {
		return
	}
	b, err := json.Marshal(sharedVersionMsg{Peer: peer.ID, Version: version, Generation: gen})
	if err != nil {
		slog.Error("Error encoding an OpenBridge version", "error", err)
		return
	}
	s.publishLater(s.egressOut, versionTopic, b)
}

// raiseVersion records version of generation gen for the link of peer and
// reports whether that moved the link up. It drops a version of an older
// generation than the one it holds. Only versions 4 and 5 move it, as
// DMRHub sends no other DMRE.
func (s *Server) raiseVersion(peer uint, version uint8, gen int64) bool {
	if s.versions == nil || (version != 4 && version != enhancedVersion) {
		return false
	}
	raised := false
	s.versions.Compute(peer, func(old peerVersion, loaded bool) (peerVersion, xsync.ComputeOp) {
		if loaded && (old.generation > gen || (old.generation == gen && old.version >= version)) {
			return old, xsync.CancelOp
		}
		raised = true
		return peerVersion{version: version, generation: gen}, xsync.UpdateOp
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
			s.raiseVersion(m.Peer, m.Version, m.Generation)
		}
	}
}

func versionKey(peer uint, gen int64) string {
	return versionKeyPrefix + strconv.FormatUint(uint64(peer), 10) + ":" + strconv.FormatInt(gen, 10)
}

// parseVersionKey returns the peer and the generation of a key of
// versionKey.
func parseVersionKey(key string) (uint, int64, bool) {
	id, gen, ok := strings.Cut(strings.TrimPrefix(key, versionKeyPrefix), ":")
	if !ok {
		return 0, 0, false
	}
	peer, err := strconv.ParseUint(id, 10, 32)
	if err != nil {
		return 0, 0, false
	}
	g, err := strconv.ParseInt(gen, 10, 64)
	if err != nil {
		return 0, 0, false
	}
	return uint(peer), g, true
}

// forgetVersion drops the versions that peer named for settings that
// changed since, after the peer table was read again.
func (s *Server) forgetVersion(ctx context.Context, peer uint) {
	table, err := s.peerTable()
	if err != nil {
		slog.Error("Error reading the peers to forget an OpenBridge version", "peerID", peer, "error", err)
		return
	}
	current := int64(math.MaxInt64)
	if k, ok := table.byID[peer]; ok {
		current = generation(k.Peer)
	}
	if s.versions != nil {
		s.versions.Compute(peer, func(old peerVersion, loaded bool) (peerVersion, xsync.ComputeOp) {
			if loaded && old.generation < current {
				return old, xsync.DeleteOp
			}
			return old, xsync.CancelOp
		})
	}
	if s.kv == nil {
		return
	}
	s.scanVersions(ctx, versionKeyPrefix+strconv.FormatUint(uint64(peer), 10)+":*", func(key string, _ uint, gen int64) {
		if gen >= current {
			return
		}
		if err := s.kv.Delete(ctx, key); err != nil {
			slog.Error("Error forgetting an OpenBridge version", "peerID", peer, "error", err)
		}
	})
}

// loadVersions takes the link versions that the replicas stored, so that a
// replica that starts after a peer named its version sends that version.
func (s *Server) loadVersions(ctx context.Context) {
	if s.kv == nil {
		return
	}
	s.scanVersions(ctx, versionKeyPrefix+"*", func(key string, peer uint, gen int64) {
		v, err := s.kv.Get(ctx, key)
		if err != nil || len(v) != 1 {
			return
		}
		s.raiseVersion(peer, v[0], gen)
	})
}

// scanVersions calls fn for each stored version key that matches match.
func (s *Server) scanVersions(ctx context.Context, match string, fn func(key string, peer uint, gen int64)) {
	var cursor uint64
	for {
		keys, next, err := s.kv.Scan(ctx, cursor, match, 1000)
		if err != nil {
			slog.Error("Error listing the OpenBridge versions", "error", err)
			return
		}
		for _, key := range keys {
			if peer, gen, ok := parseVersionKey(key); ok {
				fn(key, peer, gen)
			}
		}
		if next == 0 {
			return
		}
		cursor = next
	}
}
