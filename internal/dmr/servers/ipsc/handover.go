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

package ipsc

import (
	"encoding/json"
	"log/slog"
	"math"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// A peer that moves from a draining replica to this one in a call gets the
// rest of the call from here. The draining replica paces its packets to the
// peer, so some that it took may not have gone out when the peer moved, or
// went out after it and were lost, and the peer counts on the call control,
// RTP sequence and timestamp of the call going on as they were. So the
// draining replica reports, for each call, the state of its translator
// after the last packet that it sent to the peer before the move, and the
// packets that the peer did not get, and this replica holds what it has
// for the peer until the report comes, then sends the peer both, in order
// and each once, going on from that state.
//
// The draining replica sends an early report at the claim and the full one
// once its hub stopped the subscriptions of the peer, handoverSettle
// later, as it may still get packets that were published before this
// replica subscribed. A packet that this replica holds goes out at the
// early report when it follows the last packet of its call that the
// report knows, by sequence, as no packet that only the full report can
// hold is then before it. The other packets of its slot wait for the full
// report, so that the calls of a slot keep their order. A packet that
// lands here in a call then does not wait handoverSettle and miss its slot
// at the peer.
const (
	// handoverWait is the longest time this replica holds the packets to a
	// peer that moved while it waits for the report.
	handoverWait = time.Second
	// handedGrace is how long after a handover the packets that it sent
	// are not sent again when the hub of this replica hands them over too,
	// late.
	handedGrace = time.Second
	// sentKeep is how many packets per peer and slot the pacer remembers
	// that it sent.
	sentKeep = 64
)

// streamSnapshot is the state of the translator of a call to a peer.
type streamSnapshot struct {
	CallControl  uint32  `json:"callControl"`
	RTPSeq       uint16  `json:"rtpSeq"`
	RTPTimestamp uint32  `json:"rtpTimestamp"`
	CallSeq      uint8   `json:"callSeq"`
	LC           [9]byte `json:"lc"`
	HaveLC       bool    `json:"haveLC"`
	// Ended is a call whose terminator was sent.
	Ended bool `json:"ended"`
}

// handedCall is the last packet of a call that the draining replica sent
// to the peer before it moved, and the state after it.
type handedCall struct {
	Slot   bool           `json:"slot"`
	Stream uint           `json:"stream"`
	Seq    uint           `json:"seq"`
	State  streamSnapshot `json:"state"`
}

// handedHang is the rest of the call hang of the peer on a slot, which
// holds the data and CSBK calls to it.
type handedHang struct {
	Slot bool          `json:"slot"`
	Left time.Duration `json:"left"`
}

type handoverReport struct {
	Calls []handedCall `json:"calls"`
	Hangs []handedHang `json:"hangs,omitempty"`
	// Lost holds the packets, in msgp, that the peer did not get.
	Lost [][]byte `json:"lost"`
}

// sentPacket is a packet that the pacer sent to a peer.
type sentPacket struct {
	at    time.Time
	pkt   models.Packet
	state streamSnapshot
}

type packetKey struct {
	slot   bool
	stream uint
	seq    uint
}

func keyOf(p models.Packet) packetKey {
	return packetKey{slot: p.Slot, stream: p.StreamID, seq: p.Seq}
}

// streamKey is a call to a peer, by its slot and stream.
type streamKey struct {
	slot   bool
	stream uint
}

// moving is a peer that moved here from a draining replica, whose packets
// wait for the report of that replica, and after it the packets that went
// to the peer then, until the grace ends. Once the early report came, last
// holds the sequence of the last packet of each call that went to the
// peer, and blocked the slots whose packets wait for the full report.
type moving struct {
	held    []models.Packet
	early   bool
	done    bool
	seen    map[packetKey]bool
	last    map[streamKey]uint
	blocked [2]bool
	grace   time.Time
}

func newMoving() *moving {
	return &moving{seen: map[packetKey]bool{}, last: map[streamKey]uint{}}
}

func slotOf(p models.Packet) int {
	if p.Slot {
		return 1
	}
	return 0
}

// follows reports whether p is the packet after the last one of its call
// that went to the peer. The sequence of a packet has 8 bits.
func (m *moving) follows(p models.Packet) bool {
	l, ok := m.last[streamKey{slot: p.Slot, stream: p.StreamID}]
	return ok && p.Seq&0xff == (l+1)&0xff
}

// handOverPeer reports to the replica that claimed the peer id what this
// one sent to it before since, when the peer moved there, and stops
// sending to it. It reports at once what it knows, and again in full once
// the hub stopped the subscriptions of the peer.
func (s *IPSCServer) handOverPeer(id uint, since time.Time) {
	if id > math.MaxUint32 {
		return
	}
	peer := uint32(id)
	s.mu.Lock()
	_, here := s.peers[peer]
	delete(s.peers, peer)
	s.mu.Unlock()
	if !here || s.hub == nil {
		return
	}
	calls, lost, hangs := s.pacer.handOver(peer, since)
	report := handoverReport{Calls: calls, Hangs: hangs, Lost: marshalPackets(nil, lost)}
	if b, err := json.Marshal(report); err == nil {
		s.hub.HandOverEarly(models.RepeaterTypeIPSC, id, b)
	}
	s.hub.StopFor(models.RepeaterTypeIPSC, id)
	_, lost, report.Hangs = s.pacer.handOver(peer, since)
	report.Lost = marshalPackets(report.Lost, lost)
	b, err := json.Marshal(report)
	if err != nil {
		slog.Error("Error encoding an IPSC handover", "peerID", id, "error", err)
		return
	}
	s.hub.HandOver(models.RepeaterTypeIPSC, id, b)
	slog.Info("IPSC peer moved to another replica", "peerID", id, "calls", len(report.Calls))
}

func marshalPackets(out [][]byte, pkts []models.Packet) [][]byte {
	for _, p := range pkts {
		if b, err := p.MarshalMsg(nil); err == nil {
			out = append(out, b)
		}
	}
	return out
}

func unmarshalPackets(raws [][]byte) []models.Packet {
	out := make([]models.Packet, 0, len(raws))
	for _, raw := range raws {
		var p models.Packet
		if _, err := p.UnmarshalMsg(raw); err == nil {
			out = append(out, p)
		}
	}
	return out
}

// awaitHandover holds the packets to the peer id, which moves here, until
// the reports of the replica that served it come or handoverWait passes.
// since is when the packet of the peer that moved it here came. Call it
// before the claim of the peer.
func (s *IPSCServer) awaitHandover(id uint32, since time.Time) {
	var early pubsub.Subscription
	var wg sync.WaitGroup
	wg.Go(func() { early = s.hub.AwaitHandOverEarly(models.RepeaterTypeIPSC, uint(id)) })
	sub := s.hub.AwaitHandOver(models.RepeaterTypeIPSC, uint(id))
	wg.Wait()
	var earlyCh <-chan []byte
	if early != nil {
		earlyCh = early.Channel()
	}
	if sub == nil {
		if early != nil {
			_ = early.Close()
		}
		return
	}
	s.movingMu.Lock()
	if s.moving == nil {
		s.moving = map[uint32]*moving{}
	}
	s.moving[id] = newMoving()
	s.movingMu.Unlock()
	go func() {
		defer func() { _ = sub.Close() }()
		if early != nil {
			defer func() { _ = early.Close() }()
		}
		t := s.clock.NewTimer(handoverWait)
		defer t.Stop()
		for {
			var report *handoverReport
			select {
			case <-s.done:
			case <-t.C():
				slog.Warn("No handover came for an IPSC peer that moved here", "peerID", id)
			case b, ok := <-earlyCh:
				earlyCh = nil
				var r handoverReport
				if ok && json.Unmarshal(b, &r) == nil {
					s.startHandover(id, since, &r)
				}
				continue
			case b, ok := <-sub.Channel():
				var r handoverReport
				if ok && json.Unmarshal(b, &r) == nil {
					report = &r
				}
			}
			s.finishHandover(id, since, report)
			return
		}
	}()
}

// applyReport holds the slots of the peer id for the call hangs of report,
// and, when calls is set, makes the calls to the peer go on from the state
// of the report.
func (s *IPSCServer) applyReport(id uint32, report *handoverReport, calls bool) {
	for _, h := range report.Hangs {
		s.pacer.holdFor(slotKey{peer: id, slot: h.Slot}, min(h.Left, s.pacer.longestHold()))
	}
	if !calls {
		return
	}
	t := s.egressTranslator(id)
	for _, c := range report.Calls {
		if !c.State.Ended && c.Stream <= math.MaxUint32 {
			t.Continue(uint32(c.Stream), c.State)
		}
	}
}

// startHandover makes the calls to the peer id go on from the state of the
// early report, sends the peer the packets that the report holds, and then
// those held for it that follow the last packet of their call, in order
// and each once.
func (s *IPSCServer) startHandover(id uint32, since time.Time, report *handoverReport) {
	s.movingMu.Lock()
	defer s.movingMu.Unlock()
	m, ok := s.moving[id]
	if !ok || m.early || m.done {
		return
	}
	m.early = true
	s.applyReport(id, report, true)
	for _, c := range report.Calls {
		m.last[streamKey{slot: c.Slot, stream: c.Stream}] = c.Seq
	}
	s.sendHanded(id, m, unmarshalPackets(report.Lost), since)
	held := m.held
	m.held = nil
	for _, p := range held {
		s.forward(id, m, p)
	}
}

// finishHandover makes the calls to the peer id go on from the state of
// the report, unless an early report did, and sends the peer the packets
// that the report holds and then those held for it, in order and each
// once. The packets of the report were due on the grid of the other
// replica before since, when the peer moved, so they go out as late as
// they are, and the packets after them keep their time.
func (s *IPSCServer) finishHandover(id uint32, since time.Time, report *handoverReport) {
	s.movingMu.Lock()
	defer s.movingMu.Unlock()
	m, ok := s.moving[id]
	if ok && m.done {
		return
	}
	var batch []models.Packet
	if report != nil {
		s.applyReport(id, report, !ok || !m.early)
		batch = unmarshalPackets(report.Lost)
	}
	if !ok {
		return
	}
	s.sendHanded(id, m, batch, since)
	for _, p := range m.held {
		if k := keyOf(p); !m.seen[k] {
			m.seen[k] = true
			s.sendTo(id, p)
		}
	}
	m.held = nil
	m.done, m.grace = true, s.clock.Now().Add(handedGrace)
}

// sendHanded sends the peer id the packets of a report that it did not get
// from here yet, as due before since. The caller holds s.movingMu.
func (s *IPSCServer) sendHanded(id uint32, m *moving, batch []models.Packet, since time.Time) {
	fresh := batch[:0]
	for _, p := range batch {
		if k := keyOf(p); !m.seen[k] {
			m.seen[k] = true
			fresh = append(fresh, p)
		}
	}
	due := since.Add(-time.Duration(len(fresh)) * burstTime)
	for _, p := range fresh {
		m.last[streamKey{slot: p.Slot, stream: p.StreamID}] = p.Seq
		s.sendToDue(id, p, due)
		due = due.Add(burstTime)
	}
}

// forward sends p to the peer id, which waits for a handover, when the
// early report came and p follows the last packet of its call, and holds
// it otherwise. The caller holds s.movingMu.
func (s *IPSCServer) forward(id uint32, m *moving, p models.Packet) {
	k := keyOf(p)
	switch {
	case m.seen[k]:
	case m.early && !m.blocked[slotOf(p)] && m.follows(p):
		m.seen[k] = true
		m.last[streamKey{slot: p.Slot, stream: p.StreamID}] = p.Seq
		s.sendTo(id, p)
	default:
		if m.early {
			m.blocked[slotOf(p)] = true
		}
		m.held = append(m.held, p)
	}
}

// route sends rp to its peer, or holds it while the peer waits for a
// handover.
func (s *IPSCServer) route(rp hub.RoutedPacket) {
	id := uint32(rp.RepeaterID) //nolint:gosec
	s.movingMu.Lock()
	if m, ok := s.moving[id]; ok {
		switch {
		case !m.done:
			s.forward(id, m, rp.Packet)
			s.movingMu.Unlock()
			return
		case s.clock.Now().After(m.grace):
			delete(s.moving, id)
		case m.seen[keyOf(rp.Packet)]:
			s.movingMu.Unlock()
			return
		}
	}
	s.movingMu.Unlock()
	s.sendTo(id, rp.Packet)
}
