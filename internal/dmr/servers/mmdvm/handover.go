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

package mmdvm

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
)

// A repeater that moves from a draining replica to this one reaches this
// one with its next packet, but the draining replica sent it what came in
// until it learned of the move, and the load balancer no longer took that
// to the repeater. So the draining replica hands over the packets that it
// sent after the move, and this one holds what it has for the repeater
// until they come, then sends both in order, each once.
const (
	// handoverWait is the longest time this server holds the packets to a
	// repeater that moved here while it waits for the handover.
	handoverWait = time.Second
	// handedGrace is how long after a handover the packets that it sent
	// are not sent again when the hub of this replica hands them over too,
	// late.
	handedGrace = time.Second
	// sentKeep is how many of the last packets to each repeater the
	// server remembers.
	sentKeep = 64
	// wtHandoverGap is the time between two packets of a handover to a
	// browser hotspot. Its WebTransport session holds only a few datagrams
	// that the browser has not read and drops the rest, as quic-go does
	// past 32.
	wtHandoverGap = 2 * time.Millisecond
)

// MoveSlack is how long before the claim of an MMDVM repeater that moved
// the old replica hands over what it sent to it. MMDVMHost and DMRGateway
// drop a frame from the network only when it repeats the last one; they
// play a copy of an earlier frame as audio after the silence that they
// insert for it. So of the frames that left in that time before the claim,
// the handover holds only the last one on each slot: frames go out 60 ms
// apart in a call, but may leave closer together after a stall. When the
// load balancer takes more than MoveSlack to pass on the packet that moved
// the repeater, or two frames on a slot leave in MoveSlack and the load
// balancer drops both, the repeater loses the frames that the handover
// does not hold. A browser hotspot drops all copies, so its handover holds
// all that left in hub.MoveSlack.
const MoveSlack = 60 * time.Millisecond

// handoverRule is what a handover holds of the packets that left before
// the claim: those that left from slack before it, and with last only the
// last of them on each slot.
type handoverRule struct {
	slack time.Duration
	last  bool
}

// ruleFor returns the handoverRule of a repeater at ep.
func ruleFor(ep endpoint) handoverRule {
	if ep.transport == webTransportName {
		return handoverRule{slack: hub.MoveSlack}
	}
	return handoverRule{slack: MoveSlack, last: true}
}

// sentPacket is a packet to a repeater, and at is when it left, or zero
// while it waits in the queue.
type sentPacket struct {
	at  time.Time
	pkt models.Packet
}

// handover is what a draining replica sent to a repeater after it moved.
type handover struct {
	Packets [][]byte `json:"packets"`
}

// movingRepeater is a repeater that moved here, whose packets wait for the
// handover of the replica that served it, and after it the packets that
// went to it then, until the grace ends.
type movingRepeater struct {
	held  []models.Packet
	done  bool
	seen  map[packetKey]bool
	grace time.Time
}

type packetKey struct {
	slot   bool
	stream uint
	seq    uint
}

func keyOf(p models.Packet) packetKey {
	return packetKey{slot: p.Slot, stream: p.StreamID, seq: p.Seq}
}

// remember keeps pkt, which goes to the repeater id, and returns the
// record of it, which the writer stamps when it leaves.
func (s *Server) remember(id uint, pkt models.Packet) *sentPacket {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	if s.sent == nil {
		s.sent = map[uint][]*sentPacket{}
	}
	list := s.sent[id]
	if len(list) == sentKeep {
		list = list[1:]
	}
	p := &sentPacket{pkt: pkt}
	s.sent[id] = append(list, p)
	return p
}

// forgetPacket forgets p, which never went to the repeater id.
func (s *Server) forgetPacket(id uint, p *sentPacket) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	list := s.sent[id]
	for i, x := range list {
		if x == p {
			s.sent[id] = append(list[:i:i], list[i+1:]...)
			return
		}
	}
}

// sentSince returns the packets to the repeater id that left at or after
// since, or that still wait in the queue, and those that the rule holds of
// the ones that left before since, in order, and forgets the repeater. A
// packet that waits leaves after the move, which the load balancer no
// longer takes to the repeater.
func (s *Server) sentSince(id uint, since time.Time, rule handoverRule) []models.Packet {
	from := since.Add(-rule.slack)
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	list := s.sent[id]
	lastOn := map[bool]int{}
	for i, p := range list {
		if !p.at.IsZero() && p.at.Before(since) && !p.at.Before(from) {
			lastOn[p.pkt.Slot] = i
		}
	}
	var out []models.Packet
	for i, p := range list {
		switch {
		case p.at.IsZero() || !p.at.Before(since):
			out = append(out, p.pkt)
		case p.at.Before(from):
		case !rule.last || lastOn[p.pkt.Slot] == i:
			out = append(out, p.pkt)
		}
	}
	delete(s.sent, id)
	return out
}

// handOver sends the replica that claimed the repeater id the packets that
// this one sent to it after since, when its packet reached the other
// replica, and those that the rule holds of the ones before.
func (s *Server) handOver(id uint, since time.Time, rule handoverRule) {
	if s.hub == nil {
		return
	}
	lost := s.sentSince(id, since, rule)
	h := handover{Packets: make([][]byte, 0, len(lost))}
	for _, p := range lost {
		b, err := p.MarshalMsg(nil)
		if err != nil {
			continue
		}
		h.Packets = append(h.Packets, b)
	}
	b, err := json.Marshal(h)
	if err != nil {
		slog.Error("Error encoding an MMDVM handover", "repeaterID", id, "error", err)
		return
	}
	s.hub.HandOver(models.RepeaterTypeMMDVM, id, b)
}

// awaitHandover holds the packets to the repeater id, which moves here,
// until the handover of the replica that served it comes or handoverWait
// passes. Call it before the claim of the repeater.
func (s *Server) awaitHandover(ctx context.Context, id uint) {
	if s.hub == nil {
		return
	}
	sub := s.hub.AwaitHandOver(models.RepeaterTypeMMDVM, id)
	if sub == nil {
		return
	}
	s.movingMu.Lock()
	if s.moving == nil {
		s.moving = map[uint]*movingRepeater{}
	}
	s.moving[id] = &movingRepeater{seen: map[packetKey]bool{}}
	s.movingMu.Unlock()
	go func() {
		defer func() { _ = sub.Close() }()
		t := s.clock.NewTimer(handoverWait)
		defer t.Stop()
		var lost []models.Packet
		select {
		case <-s.done:
		case <-t.C():
			slog.Warn("No handover came for a repeater that moved here", "repeaterID", id)
		case b, ok := <-sub.Channel():
			var h handover
			if ok && json.Unmarshal(b, &h) == nil {
				for _, raw := range h.Packets {
					var p models.Packet
					if _, err := p.UnmarshalMsg(raw); err == nil {
						lost = append(lost, p)
					}
				}
			}
		}
		s.finishHandover(context.WithoutCancel(ctx), id, lost)
	}()
}

// finishHandover sends the repeater id the packets of the handover and
// then those held for it, in order and each once, wtHandoverGap apart to a
// browser hotspot. Packets that come for the repeater meanwhile are held
// too and sent after them.
func (s *Server) finishHandover(ctx context.Context, id uint, lost []models.Packet) {
	s.movingMu.Lock()
	m, ok := s.moving[id]
	s.movingMu.Unlock()
	if !ok {
		return
	}
	var gap time.Duration
	if ep, ok := s.connected.Load(id); ok && ep.transport == webTransportName {
		gap = wtHandoverGap
	}
	sent := false
	batch := lost
	for {
		for _, p := range batch {
			if m.seen[keyOf(p)] {
				continue
			}
			m.seen[keyOf(p)] = true
			if sent && gap > 0 && !s.wait(gap) {
				return
			}
			sent = true
			s.deliver(ctx, id, p)
		}
		s.movingMu.Lock()
		if len(m.held) == 0 {
			m.done, m.grace = true, s.clock.Now().Add(handedGrace)
			s.movingMu.Unlock()
			return
		}
		batch, m.held = m.held, nil
		s.movingMu.Unlock()
	}
}

// wait waits for d, and reports false when the server stops first.
func (s *Server) wait(d time.Duration) bool {
	t := s.clock.NewTimer(d)
	defer t.Stop()
	select {
	case <-s.done:
		return false
	case <-t.C():
		return true
	}
}

// resendMoved sends again to the browser hotspot at the WebTransport
// session addr what left for it from hub.MoveSlack before at, wtHandoverGap
// apart, when the load balancer moved the QUIC connection of the session
// to another path then. The load balancer drops what this replica sends
// on the old path until it learns of the new one, as at a move of the
// hotspot, and the hotspot drops what it got already.
func (s *Server) resendMoved(addr wtAddr, at time.Time) {
	ep := endpoint{transport: webTransportName, addr: addr}
	var id uint
	found := false
	s.connected.Range(func(k uint, e endpoint) bool {
		if e == ep {
			id, found = k, true
		}
		return !found
	})
	if !found {
		return
	}
	pkts := s.leftSince(id, at.Add(-ruleFor(ep).slack))
	if len(pkts) == 0 {
		return
	}
	slog.Debug("Sending again what the load balancer may have lost at a path move", "repeaterID", id, "session", addr, "packets", len(pkts))
	go func() {
		for i, p := range pkts {
			if i > 0 && !s.wait(wtHandoverGap) {
				return
			}
			if cur, ok := s.connected.Load(id); !ok || cur != ep {
				return
			}
			select {
			case s.outgoingChan <- outgoing{raw: rawPacketFrom(ep, p.Encode())}:
			case <-s.done:
				return
			}
		}
	}()
}

// leftSince returns the packets to the repeater id that left at or after
// since, in order.
func (s *Server) leftSince(id uint, since time.Time) []models.Packet {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	var out []models.Packet
	for _, p := range s.sent[id] {
		if !p.at.IsZero() && !p.at.Before(since) {
			out = append(out, p.pkt)
		}
	}
	return out
}

// forgetSent forgets the packets sent to the repeater id.
func (s *Server) forgetSent(id uint) {
	s.sentMu.Lock()
	defer s.sentMu.Unlock()
	delete(s.sent, id)
}

// hold keeps pkt for the repeater id while it waits for a handover, and
// reports whether it did. After the handover it reports true for a packet
// that the handover already sent, which comes late, so that it is dropped.
func (s *Server) hold(id uint, pkt models.Packet) bool {
	s.movingMu.Lock()
	defer s.movingMu.Unlock()
	m, ok := s.moving[id]
	if !ok {
		return false
	}
	if !m.done {
		m.held = append(m.held, pkt)
		return true
	}
	if s.clock.Now().After(m.grace) {
		delete(s.moving, id)
		return false
	}
	return m.seen[keyOf(pkt)]
}
