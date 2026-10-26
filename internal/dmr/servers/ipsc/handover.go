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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
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

// moving is a peer that moved here from a draining replica, whose packets
// wait for the report of that replica, and after it the packets that went
// to the peer then, until the grace ends.
type moving struct {
	held  []models.Packet
	done  bool
	seen  map[packetKey]bool
	grace time.Time
}

// handOverPeer reports to the replica that claimed the peer id what this
// one sent to it before since, when the peer moved there, and stops
// sending to it.
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
	s.hub.StopFor(models.RepeaterTypeIPSC, id)
	calls, lost, hangs := s.pacer.handOver(peer, since)
	report := handoverReport{Calls: calls, Hangs: hangs}
	for _, p := range lost {
		if b, err := p.MarshalMsg(nil); err == nil {
			report.Lost = append(report.Lost, b)
		}
	}
	b, err := json.Marshal(report)
	if err != nil {
		slog.Error("Error encoding an IPSC handover", "peerID", id, "error", err)
		return
	}
	s.hub.HandOver(models.RepeaterTypeIPSC, id, b)
	slog.Info("IPSC peer moved to another replica", "peerID", id, "calls", len(report.Calls))
}

// awaitHandover holds the packets to the peer id, which moves here, until
// the report of the replica that served it comes or handoverWait passes.
// Call it before the claim of the peer.
func (s *IPSCServer) awaitHandover(id uint32) {
	sub := s.hub.AwaitHandOver(models.RepeaterTypeIPSC, uint(id))
	if sub == nil {
		return
	}
	s.movingMu.Lock()
	if s.moving == nil {
		s.moving = map[uint32]*moving{}
	}
	s.moving[id] = &moving{seen: map[packetKey]bool{}}
	s.movingMu.Unlock()
	go func() {
		defer func() { _ = sub.Close() }()
		t := time.NewTimer(handoverWait)
		defer t.Stop()
		var report *handoverReport
		select {
		case <-s.done:
		case <-t.C:
			slog.Warn("No handover came for an IPSC peer that moved here", "peerID", id)
		case b, ok := <-sub.Channel():
			var r handoverReport
			if ok && json.Unmarshal(b, &r) == nil {
				report = &r
			}
		}
		s.finishHandover(id, report)
	}()
}

// finishHandover makes the calls to the peer id go on from the state of
// the report, and sends the peer the packets that the report holds and then
// those held for it, in order and each once. Packets that come for the
// peer meanwhile are held too and sent after them.
func (s *IPSCServer) finishHandover(id uint32, report *handoverReport) {
	var batch []models.Packet
	if report != nil {
		for _, h := range report.Hangs {
			s.pacer.holdFor(slotKey{peer: id, slot: h.Slot}, min(h.Left, callHangHold))
		}
		t := s.egressTranslator(id)
		for _, c := range report.Calls {
			if !c.State.Ended && c.Stream <= math.MaxUint32 {
				t.Continue(uint32(c.Stream), c.State)
			}
		}
		for _, raw := range report.Lost {
			var p models.Packet
			if _, err := p.UnmarshalMsg(raw); err == nil {
				batch = append(batch, p)
			}
		}
	}
	s.movingMu.Lock()
	m, ok := s.moving[id]
	s.movingMu.Unlock()
	if !ok {
		return
	}
	for {
		for _, p := range batch {
			if m.seen[keyOf(p)] {
				continue
			}
			m.seen[keyOf(p)] = true
			s.sendTo(id, p)
		}
		s.movingMu.Lock()
		if len(m.held) == 0 {
			m.done, m.grace = true, time.Now().Add(handedGrace)
			s.movingMu.Unlock()
			return
		}
		batch, m.held = m.held, nil
		s.movingMu.Unlock()
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
			m.held = append(m.held, rp.Packet)
			s.movingMu.Unlock()
			return
		case time.Now().After(m.grace):
			delete(s.moving, id)
		case m.seen[keyOf(rp.Packet)]:
			s.movingMu.Unlock()
			return
		}
	}
	s.movingMu.Unlock()
	s.sendTo(id, rp.Packet)
}
