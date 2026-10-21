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
	"math"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
	ipscauth "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/auth"
	"github.com/USA-RedDragon/dmrgo/v2/transport/ipsc/session"
	"gorm.io/gorm"
)

// IPSCServer is the IPSC master of DMRHub. Each repeater has its own key,
// so each runs its own session.Master.
type IPSCServer struct {
	cfg        *config.Config
	db         *gorm.DB
	hub        *hub.Hub
	udp        *net.UDPConn
	mu         sync.RWMutex
	translator *IPSCTranslator
	hubHandle  *hub.ServerHandle

	localID uint32
	links   map[uint32]*link
	epoch   time.Time

	keyFor func(peerID uint32) (key [20]byte, useAuth bool, err error)

	burstHandler func(packetType byte, data []byte, addr *net.UDPAddr)

	wg       sync.WaitGroup
	done     chan struct{}
	stopped  atomic.Bool
	stopOnce sync.Once
}

type link struct {
	mu          sync.Mutex
	m           *session.Master
	key         *ipscauth.Key
	out         []session.Output
	lastSend    time.Time
	lastDBWrite time.Time
}

var (
	ErrPacketIgnored  = errors.New("packet ignored")
	ErrInvalidAuthKey = errors.New("invalid IPSC auth key")
	ErrUnknownPeer    = errors.New("unknown IPSC peer")
)

const (
	tickInterval            = time.Second
	dbWriteDebounceInterval = 30 * time.Second
	burstInterval           = 30 * time.Millisecond
)

func NewIPSCServer(cfg *config.Config, hub *hub.Hub, db *gorm.DB) *IPSCServer {
	s := &IPSCServer{
		cfg:        cfg,
		db:         db,
		hub:        hub,
		localID:    cfg.DMR.IPSC.NetworkID,
		links:      map[uint32]*link{},
		epoch:      time.Now(),
		done:       make(chan struct{}),
		translator: NewIPSCTranslator(cfg.DMR.IPSC.NetworkID),
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

func (s *IPSCServer) linkFor(peerID uint32) (*link, error) {
	s.mu.RLock()
	l, ok := s.links[peerID]
	s.mu.RUnlock()
	if ok {
		return l, nil
	}
	key, useAuth, err := s.keyFor(peerID)
	if err != nil {
		return nil, err
	}
	m, err := session.NewMaster(&session.MasterConfig{ID: s.localID, Auth: useAuth, Key: key, MaxPeers: 1})
	if err != nil {
		return nil, fmt.Errorf("error creating IPSC session: %w", err)
	}
	l = &link{m: m, out: make([]session.Output, 0, m.MaxOutputs())}
	if useAuth {
		l.key = ipscauth.NewKey(key)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if existing, ok := s.links[peerID]; ok {
		return existing, nil
	}
	s.links[peerID] = l
	return l, nil
}

func (s *IPSCServer) snapshot() map[uint32]*link {
	s.mu.RLock()
	defer s.mu.RUnlock()
	links := make(map[uint32]*link, len(s.links))
	for id, l := range s.links {
		links[id] = l
	}
	return links
}

func (s *IPSCServer) Start(ctx context.Context) error {
	var err error
	s.udp, err = net.ListenUDP("udp", &net.UDPAddr{
		IP:   net.ParseIP(s.cfg.DMR.IPSC.Bind),
		Port: s.cfg.DMR.IPSC.Port,
	})

	if err != nil {
		return fmt.Errorf("error starting UDP listener: %w", err)
	}

	s.hubHandle = s.hub.RegisterServer(hub.ServerConfig{
		Name:      models.RepeaterTypeIPSC,
		Role:      hub.RoleRepeater,
		Broadcast: true,
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

func (s *IPSCServer) Stop(_ context.Context) error {
	s.stopOnce.Do(func() {
		slog.Info("Stopping IPSC server")

		s.stopped.Store(true)
		close(s.done)

		if s.hub != nil {
			s.hub.UnregisterServer(models.RepeaterTypeIPSC)
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
	if s.stopped.Load() {
		return
	}
	for peerID, l := range s.snapshot() {
		l.mu.Lock()
		if at, ok := l.m.Deadline(); ok && now >= at {
			s.apply(ctx, peerID, l, l.m.Tick(now, l.out[:0]), false)
		}
		l.mu.Unlock()
	}
}

func (s *IPSCServer) handler(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, session.MaxPacket)
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

		s.wg.Add(1)
		go func(packetData []byte, packetAddr *net.UDPAddr) {
			defer s.wg.Done()
			if err := s.handlePacket(ctx, packetData, packetAddr); err != nil && !errors.Is(err, ErrPacketIgnored) {
				slog.Warn("error handling IPSC packet", "peer", packetAddr, "error", err, "length", len(packetData))
			}
		}(data, addr)
	}
}

func (s *IPSCServer) handlePacket(ctx context.Context, data []byte, addr *net.UDPAddr) error {
	if s.stopped.Load() {
		return ErrPacketIgnored
	}
	raw, err := ipscwire.DecodeRaw(data)
	if err != nil {
		return fmt.Errorf("packet too short for peer ID: %w", err)
	}
	from, ok := toAddr(addr)
	if !ok {
		return fmt.Errorf("peer address %v is not IPv4: %w", addr, ErrPacketIgnored)
	}
	l, err := s.linkFor(raw.ID)
	if err != nil {
		return fmt.Errorf("peer %d: %w", raw.ID, err)
	}

	l.mu.Lock()
	defer l.mu.Unlock()
	now := s.now()
	out := l.m.Packet(now, from, data, l.out[:0])
	if len(out) == 0 && l.m.Peers() == 0 && autoRegisters(raw.Opcode) && l.verified(data) {
		slog.Info("auto-registering authenticated peer", "peerID", raw.ID, "peer", addr, "packetType", byte(raw.Opcode))
		// The peer registered with another instance before a rolling
		// restart and goes on sending traffic. session.Master registers
		// such a peer only from a keepalive, so feed it the registration
		// request the peer would have sent, and drop the reply.
		req, err := ipscwire.Control{Opcode: ipscwire.OpRegisterRequest, ID: raw.ID, Mode: session.ModeDefault}.AppendBinary(nil)
		if err != nil {
			return fmt.Errorf("error building registration request: %w", err)
		}
		s.apply(ctx, raw.ID, l, l.m.Packet(now, from, l.m.Sign(req), l.out[:0]), true)
		out = l.m.Packet(now, from, data, l.out[:0])
	}
	if len(out) == 0 {
		return ErrPacketIgnored
	}
	s.apply(ctx, raw.ID, l, out, false)
	return nil
}

func autoRegisters(op ipscwire.Opcode) bool {
	switch op { //nolint:exhaustive
	case ipscwire.OpRegisterRequest, ipscwire.OpAliveRequest, ipscwire.OpDeregisterRequest,
		ipscwire.OpRegisterReply, ipscwire.OpPeerListReply, ipscwire.OpAliveReply, ipscwire.OpDeregisterReply:
		return false
	}
	return true
}

func (l *link) verified(p []byte) bool {
	if l.key == nil {
		return true
	}
	_, ok := l.key.Verify(p)
	return ok
}

// apply carries out the outputs of l.m, without its sends when quiet. The
// caller holds l.mu.
func (s *IPSCServer) apply(ctx context.Context, peerID uint32, l *link, out []session.Output, quiet bool) {
	for _, o := range out {
		switch o.Kind { //nolint:exhaustive
		case session.OutputSend:
			if quiet {
				continue
			}
			if _, err := s.udp.WriteToUDP(o.Packet, toUDPAddr(o.Addr)); err != nil {
				slog.Warn("error sending IPSC packet", "peerID", peerID, "error", err)
			}
		case session.OutputUp:
			slog.Info("IPSC peer registered", "peerID", o.ID)
			s.updateRepeaterDBTimes(l, uint(o.ID), true)
			if s.hub != nil {
				s.hub.ActivateRepeater(ctx, uint(o.ID))
			}
		case session.OutputDown:
			slog.Info("IPSC peer down", "peerID", o.ID, "reason", o.Reason)
			l.lastSend = time.Time{}
			if s.hub != nil {
				s.hub.DeactivateRepeater(ctx, uint(o.ID))
			}
		case session.OutputData:
			op := ipscwire.Opcode(o.Packet[0])
			if !isUserPacket(op) || s.burstHandler == nil {
				continue
			}
			packetCopy := make([]byte, len(o.Packet))
			copy(packetCopy, o.Packet)
			go s.burstHandler(byte(op), packetCopy, toUDPAddr(o.Addr))
		}
	}
	if l.m.Peers() > 0 {
		s.updateRepeaterDBTimes(l, uint(peerID), false)
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
// The caller holds l.mu.
func (s *IPSCServer) updateRepeaterDBTimes(l *link, repeaterID uint, isConnect bool) {
	if s.db == nil {
		return
	}
	now := time.Now()
	if !isConnect && !l.lastDBWrite.IsZero() && now.Sub(l.lastDBWrite) < dbWriteDebounceInterval {
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
	l.lastDBWrite = now
}

func toAddr(a *net.UDPAddr) (session.Addr, bool) {
	if a == nil || a.Port < 0 || a.Port > math.MaxUint16 {
		return session.Addr{}, false
	}
	ip4 := a.IP.To4()
	if ip4 == nil {
		return session.Addr{}, false
	}
	out := session.Addr{Port: uint16(a.Port)}
	copy(out.IP[:], ip4)
	return out, true
}

func toUDPAddr(a session.Addr) *net.UDPAddr {
	return &net.UDPAddr{IP: net.IPv4(a.IP[0], a.IP[1], a.IP[2], a.IP[3]), Port: int(a.Port)}
}

func (s *IPSCServer) consumeHubPackets() {
	defer s.wg.Done()
	for rp := range s.hubHandle.Packets {
		s.SendPacket(rp.Packet)
	}
}

func (s *IPSCServer) SendPacket(pkt models.Packet) {
	for _, frame := range s.translator.TranslateToIPSC(pkt) {
		s.sendPacketInternal(frame)
	}
}

func (s *IPSCServer) sendPacketInternal(data []byte) {
	if s.stopped.Load() {
		return
	}
	for peerID, l := range s.snapshot() {
		l.mu.Lock()
		if l.m.Peers() == 0 {
			l.mu.Unlock()
			continue
		}
		to := toUDPAddr(l.m.PeerAt(0).Addr)
		l.pace()
		packet := l.m.Sign(append(make([]byte, 0, len(data)+ipscauth.TrailerLen), data...))
		l.mu.Unlock()
		slog.Debug("IPSC burst sending", "peerID", peerID, "peer", to, "length", len(packet))
		if _, err := s.udp.WriteToUDP(packet, to); err != nil {
			slog.Warn("failed sending IPSC user packet", "peer", to, "error", err)
		}
	}
}

// pace waits until burstInterval after the last send to the peer. The
// caller holds l.mu.
func (l *link) pace() {
	if !l.lastSend.IsZero() {
		if elapsed := time.Since(l.lastSend); elapsed < burstInterval {
			time.Sleep(burstInterval - elapsed)
		}
	}
	l.lastSend = time.Now()
}
