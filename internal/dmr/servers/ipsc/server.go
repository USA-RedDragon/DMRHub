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
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscauth"
	ipscsession "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/ipscsession"
	"gorm.io/gorm"
)

// IPSCServer is the IPSC master of DMRHub. One ipscsession.Master serves every
// repeater, each with its own key.
type IPSCServer struct {
	cfg        *config.Config
	db         *gorm.DB
	hub        *hub.Hub
	udp        *net.UDPConn
	translator *IPSCTranslator
	hubHandle  *hub.ServerHandle

	localID uint32
	mu      sync.Mutex
	m       *ipscsession.Master
	out     []ipscsession.Output
	peers   map[uint32]*peerState
	epoch   time.Time

	keyFor func(peerID uint32) (key [20]byte, useAuth bool, err error)

	burstHandler func(packetType byte, data []byte, addr *net.UDPAddr)
	bursts       []burst

	egressMu sync.Mutex
	egress   map[uint32]*peerTranslator

	pacer *pacer

	wg       sync.WaitGroup
	done     chan struct{}
	stopped  atomic.Bool
	stopOnce sync.Once
}

type burst struct {
	op   byte
	data []byte
	addr *net.UDPAddr
}

// peerState is what DMRHub keeps for a registered peer.
type peerState struct {
	lastDBWrite time.Time
}

var (
	ErrPacketIgnored  = errors.New("packet ignored")
	ErrInvalidAuthKey = errors.New("invalid IPSC auth key")
	ErrUnknownPeer    = errors.New("unknown IPSC peer")
	errNoSession      = errors.New("no IPSC session")
)

const (
	tickInterval            = time.Second
	dbWriteDebounceInterval = 30 * time.Second
	staleStreamAge          = 30 * time.Second
)

func NewIPSCServer(cfg *config.Config, hub *hub.Hub, db *gorm.DB) *IPSCServer {
	s := &IPSCServer{
		cfg:        cfg,
		db:         db,
		hub:        hub,
		localID:    cfg.DMR.IPSC.NetworkID,
		peers:      map[uint32]*peerState{},
		epoch:      time.Now(),
		done:       make(chan struct{}),
		translator: NewIPSCTranslator(cfg.DMR.IPSC.NetworkID),
		egress:     map[uint32]*peerTranslator{},
	}
	s.pacer = newPacer(func(k slotKey, frame []byte) { s.sendPacketToPeers(frame, k.peer) })
	m, err := ipscsession.NewMaster(&ipscsession.MasterConfig{
		ID:         s.localID,
		MaxPeers:   ipscsession.MaxPeersLimit,
		AdoptKnown: true,
	})
	if err != nil {
		slog.Error("error creating IPSC session", "error", err)
	} else {
		s.m = m
		s.out = make([]ipscsession.Output, 0, m.MaxOutputs())
	}
	s.keyFor = s.lookupPeerAuthKey
	return s
}

func (s *IPSCServer) lookupPeerAuthKey(peerID uint32) ([20]byte, bool, error) {
	if s.db == nil {
		return [20]byte{}, false, fmt.Errorf("no database configured: %w", ErrUnknownPeer)
	}
	var repeater models.Repeater
	err := s.db.Where("id = ? AND type = ?", peerID, models.RepeaterTypeIPSC).First(&repeater).Error
	if err != nil {
		return [20]byte{}, false, fmt.Errorf("repeater %d not found: %w", peerID, err)
	}
	if repeater.Password == "" {
		return [20]byte{}, false, nil
	}
	key, err := ipscauth.ParseKey(repeater.Password)
	if err != nil {
		return [20]byte{}, false, fmt.Errorf("repeater %d has an auth key that is not hex: %w", peerID, ErrInvalidAuthKey)
	}
	return key, true, nil
}

// Listen opens the socket of the server. Start calls it when it was not
// called before.
func (s *IPSCServer) Listen() error {
	if s.udp != nil {
		return nil
	}
	udp, err := net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP(s.cfg.DMR.IPSC.Bind),
		Port: s.cfg.DMR.IPSC.Port,
	})
	if err != nil {
		return fmt.Errorf("error starting UDP listener: %w", err)
	}
	s.udp = udp
	return nil
}

func (s *IPSCServer) Start(ctx context.Context) error {
	if err := s.Listen(); err != nil {
		return err
	}

	s.hubHandle = s.hub.RegisterServer(hub.ServerConfig{
		Name: models.RepeaterTypeIPSC,
		Role: hub.RoleRepeater,
	})

	s.SetBurstHandler(func(packetType byte, data []byte, _ *net.UDPAddr) {
		for _, pkt := range s.translator.TranslateToMMDVM(packetType, data) {
			if s.hub != nil {
				s.hub.RoutePacket(ctx, pkt, models.RepeaterTypeIPSC)
			}
		}
	})

	s.wg.Add(3)
	go s.handler(ctx)
	go s.consumeHubPackets()
	go s.ticker(ctx)

	slog.Info("IPSC Server listening", "address", s.cfg.DMR.IPSC.Bind, "port", s.cfg.DMR.IPSC.Port)

	return nil
}

// Addr returns the server's listening address. This is useful in tests where
// the server is started on an ephemeral port (port 0).
func (s *IPSCServer) Addr() net.Addr {
	return s.udp.LocalAddr()
}

// Stop stops the server. Unless ctx asks for a graceful handoff, it first
// asks every registered peer to deregister, as DMRlink does, so that the
// peers register again.
func (s *IPSCServer) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		slog.Info("Stopping IPSC server")

		s.stopped.Store(true)
		close(s.done)

		if s.hub != nil {
			s.hub.UnregisterServer(models.RepeaterTypeIPSC)
		}
		s.pacer.drain(pacerDrainTimeout)

		if !servers.IsGracefulHandoff(ctx) {
			s.closeSessions(ctx)
		}

		if s.udp != nil {
			if err := s.udp.Close(); err != nil {
				slog.Error("error closing UDP listener", "error", err)
			}
		}
	})
	s.wg.Wait()
	return nil
}

func (s *IPSCServer) closeSessions(ctx context.Context) {
	if s.m == nil || s.udp == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.apply(ctx, s.m.Close(s.now(), s.out[:0]))
}

func (s *IPSCServer) now() time.Duration {
	return time.Since(s.epoch)
}

func (s *IPSCServer) ticker(ctx context.Context) {
	defer s.wg.Done()
	t := time.NewTicker(tickInterval)
	defer t.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-t.C:
			s.tick(ctx, s.now())
		}
	}
}

func (s *IPSCServer) tick(ctx context.Context, now time.Duration) {
	if s.stopped.Load() || s.m == nil {
		return
	}
	s.mu.Lock()
	if at, ok := s.m.Deadline(); ok && now >= at {
		s.apply(ctx, s.m.Tick(now, s.out[:0]))
	}
	bursts := s.takeBursts()
	s.mu.Unlock()
	s.runBursts(bursts)
	s.cleanupStaleStreams()
}

func (s *IPSCServer) handler(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, ipscsession.MaxPacket)
	for {
		n, addr, err := s.udp.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Warn("error reading from UDP", "error", err)
			continue
		}
		data := make([]byte, n)
		copy(data, buf[:n])

		if err := s.handlePacket(ctx, data, addr); err != nil && !errors.Is(err, ErrPacketIgnored) {
			slog.Warn("error handling IPSC packet", "peer", addr, "error", err, "length", len(data))
		}
	}
}

func (s *IPSCServer) handlePacket(ctx context.Context, data []byte, addr *net.UDPAddr) error {
	if s.stopped.Load() {
		return ErrPacketIgnored
	}
	if s.m == nil {
		return errNoSession
	}
	raw, err := ipscwire.DecodeRaw(data)
	if err != nil {
		return fmt.Errorf("packet too short for peer ID: %w", err)
	}
	if addr == nil {
		return fmt.Errorf("no peer address: %w", ErrPacketIgnored)
	}
	from, ok := ipscsession.AddrOf(addr.IP, addr.Port)
	if !ok {
		return fmt.Errorf("peer address %v is not IPv4: %w", addr, ErrPacketIgnored)
	}
	key, useAuth, err := s.keyFor(raw.ID)
	if err != nil {
		return fmt.Errorf("peer %d: %w", raw.ID, err)
	}
	var kp *[20]byte
	if useAuth {
		kp = &key
	}

	s.mu.Lock()
	now := s.now()
	out := s.m.Packet(now, from, data, kp, s.out[:0])
	if kp == nil && unregistered(out) && raw.Opcode != ipscwire.OpDeregisterRequest {
		slog.Info("adopting keyless peer", "peerID", raw.ID, "peer", addr, "packetType", byte(raw.Opcode))
		s.apply(ctx, s.m.Adopt(now, from, raw.ID, kp, s.out[:0]))
		out = s.m.Packet(now, from, data, kp, s.out[:0])
	}
	ignored := len(out) == 0 || out[0].Kind == ipscsession.OutputDropped
	s.apply(ctx, out)
	if p, ok := s.peers[raw.ID]; ok {
		s.updateRepeaterDBTimes(p, uint(raw.ID), false)
	}
	bursts := s.takeBursts()
	s.mu.Unlock()
	s.runBursts(bursts)
	if ignored {
		return ErrPacketIgnored
	}
	return nil
}

func unregistered(out []ipscsession.Output) bool {
	return len(out) == 1 && out[0].Kind == ipscsession.OutputDropped && out[0].Reason == ipscsession.ReasonUnregistered
}

// apply carries out the outputs of the Master. The caller holds s.mu.
func (s *IPSCServer) apply(ctx context.Context, out []ipscsession.Output) {
	for _, o := range out {
		switch o.Kind {
		case ipscsession.OutputSend:
			if _, err := s.udp.WriteToUDP(o.Packet, toUDPAddr(o.Addr)); err != nil {
				slog.Warn("error sending IPSC packet", "peer", toUDPAddr(o.Addr), "error", err)
			}
		case ipscsession.OutputUp:
			slog.Info("IPSC peer registered", "peerID", o.ID)
			p := &peerState{}
			s.peers[o.ID] = p
			s.updateRepeaterDBTimes(p, uint(o.ID), true)
			if s.hub != nil {
				s.hub.ActivateRepeater(ctx, uint(o.ID))
			}
		case ipscsession.OutputDown:
			slog.Info("IPSC peer down", "peerID", o.ID, "reason", o.Reason)
			delete(s.peers, o.ID)
			if s.hub != nil {
				s.hub.DeactivateRepeater(ctx, uint(o.ID))
			}
		case ipscsession.OutputDropped:
			slog.Debug("IPSC packet dropped", "peerID", o.ID, "peer", toUDPAddr(o.Addr), "reason", o.Reason)
		case ipscsession.OutputData:
			op := ipscwire.Opcode(o.Packet[0])
			if !isUserPacket(op) || s.burstHandler == nil {
				continue
			}
			s.bursts = append(s.bursts, burst{op: byte(op), data: append([]byte(nil), o.Packet...), addr: toUDPAddr(o.Addr)})
		case ipscsession.OutputPeers:
		}
	}
}

func (s *IPSCServer) takeBursts() []burst {
	b := s.bursts
	s.bursts = nil
	return b
}

func (s *IPSCServer) runBursts(bursts []burst) {
	for _, b := range bursts {
		s.burstHandler(b.op, b.data, b.addr)
	}
}

func isUserPacket(op ipscwire.Opcode) bool {
	switch op { //nolint:exhaustive
	case ipscwire.OpGroupVoice, ipscwire.OpPrivateVoice, ipscwire.OpGroupData, ipscwire.OpPrivateData:
		return true
	}
	return false
}

func (s *IPSCServer) SetBurstHandler(handler func(packetType byte, data []byte, addr *net.UDPAddr)) {
	s.burstHandler = handler
}

// updateRepeaterDBTimes writes the LastPing of the repeater, and on connect
// its Connected time, at most once per dbWriteDebounceInterval otherwise.
// The caller holds s.mu.
func (s *IPSCServer) updateRepeaterDBTimes(p *peerState, repeaterID uint, isConnect bool) {
	if s.db == nil {
		return
	}
	now := time.Now()
	if !isConnect && !p.lastDBWrite.IsZero() && now.Sub(p.lastDBWrite) < dbWriteDebounceInterval {
		return
	}
	dbRepeater, err := models.FindRepeaterByID(s.db, repeaterID)
	if err != nil {
		slog.Debug("IPSC: repeater not found in DB for time update", "repeaterID", repeaterID)
		return
	}
	dbRepeater.LastPing = now
	if isConnect {
		dbRepeater.Connected = now
	}
	if err := s.db.Save(&dbRepeater).Error; err != nil {
		slog.Error("IPSC: error saving repeater times", "repeaterID", repeaterID, "error", err)
		return
	}
	p.lastDBWrite = now
}

func toUDPAddr(a ipscsession.Addr) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(a.IP[0], a.IP[1], a.IP[2], a.IP[3]), Port: int(a.Port)}
}

func (s *IPSCServer) consumeHubPackets() {
	defer s.wg.Done()
	for rp := range s.hubHandle.Packets {
		s.sendTo(uint32(rp.RepeaterID), rp.Packet) //nolint:gosec
	}
}

// sendTo sends pkt to the IPSC peer whose ID is repeaterID. Each peer has
// its own translator, so one stream sent to two peers keeps two states.
func (s *IPSCServer) sendTo(repeaterID uint32, pkt models.Packet) {
	for _, frame := range s.egressTranslator(repeaterID).TranslateToIPSC(pkt) {
		s.pacer.enqueue(slotKey{peer: repeaterID, slot: pkt.Slot}, frame)
	}
}

// peerTranslator is the translator of one IPSC peer, with the time it
// was last handed out.
type peerTranslator struct {
	t    *IPSCTranslator
	used time.Time
}

func (s *IPSCServer) egressTranslator(repeaterID uint32) *IPSCTranslator {
	s.egressMu.Lock()
	defer s.egressMu.Unlock()
	e, ok := s.egress[repeaterID]
	if !ok {
		if s.egress == nil {
			s.egress = map[uint32]*peerTranslator{}
		}
		t := NewIPSCTranslator(s.localID)
		if s.hub != nil {
			t.onUndeliverable = func(pkt models.Packet, reason string) {
				s.hub.ReportUndeliverable(pkt, models.RepeaterTypeIPSC, reason)
			}
		}
		e = &peerTranslator{t: t}
		s.egress[repeaterID] = e
	}
	e.used = time.Now()
	return e.t
}

func (s *IPSCServer) cleanupStaleStreams() {
	if s.translator != nil {
		s.translator.CleanupStaleStreams(staleStreamAge)
	}
	s.egressMu.Lock()
	defer s.egressMu.Unlock()
	for id, e := range s.egress {
		e.t.CleanupStaleStreams(staleStreamAge)
		if forward, reverse := e.t.StreamCount(); forward == 0 && reverse == 0 && time.Since(e.used) > staleStreamAge {
			delete(s.egress, id)
		}
	}
}

type destination struct {
	id     uint32
	to     *net.UDPAddr
	packet []byte
}

func (s *IPSCServer) sendPacketInternal(data []byte) {
	s.sendPacketToPeers(data, 0)
}

// sendPacketToPeers sends data to the registered peer only, or to every
// registered peer when only is 0.
func (s *IPSCServer) sendPacketToPeers(data []byte, only uint32) {
	if s.stopped.Load() || s.m == nil {
		return
	}
	s.mu.Lock()
	dests := make([]destination, 0, len(s.peers))
	for id := range s.peers {
		if only != 0 && id != only {
			continue
		}
		info, ok := s.m.Peer(id)
		if !ok {
			continue
		}
		packet, err := s.m.SignFor(info.ID, append(make([]byte, 0, len(data)+ipscauth.TrailerLen), data...))
		if err != nil {
			slog.Warn("failed signing IPSC user packet", "peerID", info.ID, "error", err)
			continue
		}
		dests = append(dests, destination{id: info.ID, to: toUDPAddr(info.Addr), packet: packet})
	}
	s.mu.Unlock()
	for _, d := range dests {
		slog.Debug("IPSC burst sending", "peerID", d.id, "peer", d.to, "length", len(d.packet))
		if _, err := s.udp.WriteToUDP(d.packet, d.to); err != nil {
			slog.Warn("failed sending IPSC user packet", "peer", d.to, "error", err)
		}
	}
}
