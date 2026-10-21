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
	"gorm.io/gorm"
)

type IPSCServer struct {
	cfg        *config.Config
	db         *gorm.DB
	hub        *hub.Hub
	udp        *net.UDPConn
	mu         sync.RWMutex
	translator *IPSCTranslator
	hubHandle  *hub.ServerHandle

	localID  uint32
	peers    map[uint32]*Peer
	lastSend map[uint32]time.Time

	// lastDBWrite tracks the last time we wrote to the DB for each repeater,
	// used to debounce keepalive DB writes.
	lastDBWrite map[uint32]time.Time

	burstHandler func(packetType byte, data []byte, addr *net.UDPAddr)

	wg       sync.WaitGroup
	stopped  atomic.Bool
	stopOnce sync.Once
}

type Packet struct {
	data []byte
}

type Peer struct {
	ID                 uint32
	Addr               *net.UDPAddr
	Mode               byte
	Flags              [4]byte
	LastSeen           time.Time
	KeepAliveReceived  uint64
	RegistrationStatus bool
	AuthKey            *[20]byte // cached HMAC key for this peer
}

//nolint:gochecknoglobals
var ipscVersion = [4]byte{0x04, 0x02, 0x04, 0x01}

var (
	ErrPacketIgnored  = errors.New("packet ignored")
	ErrInvalidAuthKey = errors.New("invalid IPSC auth key")
)

func NewIPSCServer(cfg *config.Config, hub *hub.Hub, db *gorm.DB) *IPSCServer {
	return &IPSCServer{
		cfg:         cfg,
		db:          db,
		hub:         hub,
		localID:     cfg.DMR.IPSC.NetworkID,
		peers:       map[uint32]*Peer{},
		lastSend:    map[uint32]time.Time{},
		lastDBWrite: map[uint32]time.Time{},
		translator:  NewIPSCTranslator(cfg.DMR.IPSC.NetworkID),
	}
}

// decodeAuthKey decodes a hex auth key of up to 40 digits, left-padded with
// zeros as DMRlink does. It returns nil for a key that is not hex.
func decodeAuthKey(hexKey string) *[20]byte {
	key, err := ipscauth.ParseKey(hexKey)
	if err != nil {
		slog.Error("failed to decode IPSC auth key as hex", "error", err)
		return nil
	}
	return &key
}

// lookupPeerAuthKey looks up a repeater's auth key from the database.
func (s *IPSCServer) lookupPeerAuthKey(peerID uint32) (*[20]byte, error) {
	if s.db == nil {
		return nil, fmt.Errorf("no database configured")
	}
	var repeater models.Repeater
	err := s.db.Where("id = ? AND type = ?", peerID, models.RepeaterTypeIPSC).First(&repeater).Error
	if err != nil {
		return nil, fmt.Errorf("repeater %d not found: %w", peerID, err)
	}
	if repeater.Password == "" {
		return nil, nil //nolint:nilnil // A repeater without a key does not authenticate
	}
	key := decodeAuthKey(repeater.Password)
	if key == nil {
		return nil, fmt.Errorf("repeater %d has an auth key that is not hex: %w", peerID, ErrInvalidAuthKey)
	}
	return key, nil
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

	// Register with the hub as a broadcast repeater server
	s.hubHandle = s.hub.RegisterServer(hub.ServerConfig{
		Name:      models.RepeaterTypeIPSC,
		Role:      hub.RoleRepeater,
		Broadcast: true,
	})

	// Wire up the burst handler: translate IPSC→MMDVM and route via Hub.
	s.SetBurstHandler(func(packetType byte, data []byte, _ *net.UDPAddr) {
		for _, pkt := range s.translator.TranslateToMMDVM(packetType, data) {
			if s.hub != nil {
				s.hub.RoutePacket(ctx, pkt, models.RepeaterTypeIPSC)
			}
		}
	})

	s.wg.Add(1)
	go s.handler(ctx)

	// Consume routed packets from the hub
	s.wg.Add(1)
	go s.consumeHubPackets()

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

		// Unregister from the hub so we stop receiving routed packets.
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

func (s *IPSCServer) handler(ctx context.Context) {
	defer s.wg.Done()
	buf := make([]byte, 1500)
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
			packet, err := s.handlePacket(ctx, packetData, packetAddr)
			if err != nil {
				if errors.Is(err, ErrPacketIgnored) {
					return
				}
				slog.Warn("error parsing packet", "peer", packetAddr, "error", err, "length", len(packetData), "packet", packetData)
				return
			}

			slog.Debug("received packet", "peer", packetAddr, "length", len(packetData), "packet", packet)
		}(data, addr)
	}
}

func (s *IPSCServer) handlePacket(ctx context.Context, data []byte, addr *net.UDPAddr) (*Packet, error) {
	if s.stopped.Load() {
		return nil, ErrPacketIgnored
	}

	if len(data) < 1 {
		return nil, fmt.Errorf("packet too short")
	}

	packetType := data[0]

	// We always use auth

	if len(data) <= ipscauth.TrailerLen {
		return nil, fmt.Errorf("packet too short for authentication")
	}
	// Parse peer ID to look up per-peer auth key
	peerID, err := parsePeerID(data)
	if err != nil {
		return nil, fmt.Errorf("cannot parse peer ID for auth: %w", err)
	}
	authKey, err := s.lookupAndCachePeerAuthKey(peerID)
	if err != nil {
		return nil, fmt.Errorf("unknown peer %d: %w", peerID, err)
	}
	if authKey != nil && !authWithKey(data, authKey) {
		return nil, fmt.Errorf("authentication failed")
	}
	data = data[:len(data)-ipscauth.TrailerLen] // Remove the hash from the data

	s.mu.RLock()
	peer, ok := s.peers[peerID]
	registered := ok && peer.RegistrationStatus
	s.mu.RUnlock()

	if !registered && ipscwire.Opcode(packetType) != ipscwire.OpRegisterRequest {
		// The peer passed HMAC authentication but isn't registered on this
		// server instance.  This commonly happens during rolling restarts:
		// the peer was connected to another pod that terminated, and is now
		// sending keepalives/data to this pod.
		//
		// Rather than dropping the packet and sending a DeregistrationRequest
		// (which the peer may interpret as "master is leaving" and enter a
		// slow retry loop), auto-register the peer so it seamlessly
		// transitions to this instance.
		if ipscwire.Opcode(packetType) == ipscwire.OpDeregisterRequest {
			// Peer is asking to leave — nothing to auto-register.
			return nil, ErrPacketIgnored
		}
		slog.Info("auto-registering authenticated peer", "peerID", peerID, "peer", addr, "packetType", packetType)
		s.upsertPeer(ctx, peerID, addr, s.defaultModeByte(), s.defaultFlagsBytes())
	}

	switch ipscwire.Opcode(packetType) { //nolint:exhaustive
	case ipscwire.OpGroupVoice, ipscwire.OpPrivateVoice, ipscwire.OpGroupData, ipscwire.OpPrivateData:
		if err := s.handleUserPacket(ipscwire.Opcode(packetType), data, addr); err != nil {
			return nil, err
		}
	case ipscwire.OpWakeup:
		if err := s.handleRepeaterWakeUp(data, addr); err != nil {
			return nil, err
		}
	case ipscwire.OpRegisterRequest:
		if err := s.handleMasterRegisterRequest(ctx, data, addr); err != nil {
			return nil, err
		}
	case ipscwire.OpAliveRequest:
		if err := s.handleMasterAliveRequest(data, addr); err != nil {
			return nil, err
		}
	case ipscwire.OpPeerListRequest:
		if err := s.handlePeerListRequest(data, addr); err != nil {
			return nil, err
		}
	case ipscwire.OpDeregisterRequest:
		s.handleDeregistrationRequest(ctx, peerID)
		return nil, ErrPacketIgnored
	case ipscwire.OpRegisterReply, ipscwire.OpPeerListReply, ipscwire.OpAliveReply, ipscwire.OpDeregisterReply:
		// These are reply packets, we shouldn't receive them as a server, keeping quiet.
		return nil, ErrPacketIgnored
	default:
		return nil, fmt.Errorf("unknown packet type: %d", packetType)
	}

	return &Packet{data: data}, nil
}

func (s *IPSCServer) handleMasterRegisterRequest(ctx context.Context, data []byte, addr *net.UDPAddr) error {
	peerID, err := parsePeerID(data)
	if err != nil {
		return err
	}

	mode := s.defaultModeByte()
	flags := s.defaultFlagsBytes()
	if req, err := ipscwire.DecodeControl(data); err == nil {
		mode = byte(req.Mode)
		flags = req.Flags
	}

	authKey := s.upsertPeer(ctx, peerID, addr, mode, flags)

	packet := &Packet{data: s.buildMasterRegisterReply()}
	if err := s.sendPacket(packet, addr, authKey); err != nil {
		return fmt.Errorf("error sending master register reply: %w", err)
	}

	return nil
}

func (s *IPSCServer) handleMasterAliveRequest(data []byte, addr *net.UDPAddr) error {
	peerID, err := parsePeerID(data)
	if err != nil {
		return err
	}

	authKey := s.markPeerAlive(peerID, addr)

	packet := &Packet{data: s.buildMasterAliveReply()}
	if err := s.sendPacket(packet, addr, authKey); err != nil {
		return fmt.Errorf("error sending master alive reply: %w", err)
	}

	return nil
}

func (s *IPSCServer) handlePeerListRequest(data []byte, addr *net.UDPAddr) error {
	peerID, err := parsePeerID(data)
	if err != nil {
		return err
	}

	authKey, err := s.lookupAndCachePeerAuthKey(peerID)
	if err != nil {
		return err
	}

	packet := &Packet{data: s.buildPeerListReply()}
	if err := s.sendPacket(packet, addr, authKey); err != nil {
		return fmt.Errorf("error sending peer list reply: %w", err)
	}

	return nil
}

func (s *IPSCServer) handleDeregistrationRequest(ctx context.Context, peerID uint32) {
	s.mu.Lock()
	delete(s.peers, peerID)
	delete(s.lastSend, peerID)
	s.mu.Unlock()
	slog.Info("IPSC peer deregistered", "peerID", peerID)
	// Cancel hub subscriptions for the disconnected peer
	if s.hub != nil {
		s.hub.DeactivateRepeater(ctx, uint(peerID))
	}
}

func (s *IPSCServer) handleRepeaterWakeUp(data []byte, addr *net.UDPAddr) error {
	peerID, err := parsePeerID(data)
	if err != nil {
		return err
	}

	s.markPeerAlive(peerID, addr)
	slog.Debug("repeater wake-up packet received", "peer", addr, "peerID", peerID, "length", len(data))
	return nil
}

func (s *IPSCServer) handleUserPacket(packetType ipscwire.Opcode, data []byte, addr *net.UDPAddr) error {
	peerID, err := parsePeerID(data)
	if err != nil {
		return err
	}

	s.markPeerAlive(peerID, addr)
	slog.Debug("IPSC burst received", "peer", addr, "peerID", peerID, "packetType", byte(packetType), "length", len(data))
	if s.burstHandler != nil {
		packetCopy := make([]byte, len(data))
		copy(packetCopy, data)
		go s.burstHandler(byte(packetType), packetCopy, addr)
	}
	return nil
}

func (s *IPSCServer) SetBurstHandler(handler func(packetType byte, data []byte, addr *net.UDPAddr)) {
	s.burstHandler = handler
}

// ensurePeer returns the Peer for the given ID, creating it if necessary,
// and ensures the peer's auth key is cached. The caller must hold s.mu.
func (s *IPSCServer) ensurePeer(peerID uint32, addr *net.UDPAddr) *Peer {
	peer, ok := s.peers[peerID]
	if !ok {
		peer = &Peer{ID: peerID}
		s.peers[peerID] = peer
	}
	if peer.AuthKey == nil && s.db != nil {
		key, err := s.lookupPeerAuthKey(peerID)
		if err != nil {
			slog.Warn("failed to look up auth key for peer", "peerID", peerID, "error", err)
		} else {
			peer.AuthKey = key
		}
	}
	peer.Addr = cloneUDPAddr(addr)
	peer.LastSeen = time.Now()
	return peer
}

func (s *IPSCServer) upsertPeer(ctx context.Context, peerID uint32, addr *net.UDPAddr, mode byte, flags [4]byte) *[20]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	peer := s.ensurePeer(peerID, addr)
	peer.Mode = mode
	peer.Flags = flags
	peer.RegistrationStatus = true

	// Update DB with connected time and last ping (force write on connect)
	s.updateRepeaterDBTimes(uint(peerID), true)

	// Activate hub subscriptions for this peer
	if s.hub != nil {
		s.hub.ActivateRepeater(ctx, uint(peerID))
	}

	return peer.AuthKey
}

func (s *IPSCServer) markPeerAlive(peerID uint32, addr *net.UDPAddr) *[20]byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	peer := s.ensurePeer(peerID, addr)
	peer.KeepAliveReceived++

	// Update DB with last ping time (debounced)
	s.updateRepeaterDBTimes(uint(peerID), false)

	return peer.AuthKey
}

// dbWriteDebounceInterval is the minimum time between DB writes for the same
// repeater's keepalive timestamps. This prevents excessive DB pressure from
// frequent keepalive packets.
const dbWriteDebounceInterval = 30 * time.Second

// updateRepeaterDBTimes updates the repeater's DB record with connection/ping times.
// When isConnect is false (keepalive), writes are debounced to at most once per
// dbWriteDebounceInterval. Connection events always write immediately.
func (s *IPSCServer) updateRepeaterDBTimes(repeaterID uint, isConnect bool) {
	if s.db == nil {
		return
	}

	peerID := uint32(repeaterID) //nolint:gosec // repeaterID is always within uint32 range
	now := time.Now()

	if !isConnect {
		if last, ok := s.lastDBWrite[peerID]; ok && now.Sub(last) < dbWriteDebounceInterval {
			return
		}
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
	s.lastDBWrite[peerID] = now
}

func (s *IPSCServer) buildMasterRegisterReply() []byte {
	numPeers := min(s.peerCount(), math.MaxUint16)
	reply := ipscwire.Control{
		Opcode:  ipscwire.OpRegisterReply,
		ID:      s.localID,
		Mode:    ipscwire.Mode(s.defaultModeByte()),
		Flags:   s.defaultFlagsBytes(),
		Peers:   uint16(numPeers), //nolint:gosec // Bounds checked
		Version: ipscVersion,
	}
	packet, err := reply.AppendBinary(nil)
	if err != nil {
		slog.Error("failed to encode master register reply", "error", err)
	}
	return packet
}

func (s *IPSCServer) buildMasterAliveReply() []byte {
	reply := ipscwire.Control{
		Opcode:  ipscwire.OpAliveReply,
		ID:      s.localID,
		Mode:    ipscwire.Mode(s.defaultModeByte()),
		Flags:   s.defaultFlagsBytes(),
		Version: ipscVersion,
	}
	packet, err := reply.AppendBinary(nil)
	if err != nil {
		slog.Error("failed to encode master alive reply", "error", err)
	}
	return packet
}

func (s *IPSCServer) buildPeerListReply() []byte {
	return ipscwire.AppendPeerListReply(nil, s.localID, s.buildPeerList())
}

func (s *IPSCServer) buildPeerList() []ipscwire.PeerEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.peers) == 0 {
		return nil
	}

	peerList := make([]ipscwire.PeerEntry, 0, len(s.peers))
	for _, peer := range s.peers {
		if peer.Addr == nil || peer.Addr.IP == nil {
			continue
		}
		entry := ipscwire.PeerEntry{ID: peer.ID, Mode: ipscwire.Mode(peer.Mode)}
		copy(entry.IP[:], peer.Addr.IP.To4())
		if peer.Addr.Port >= 0 && peer.Addr.Port <= math.MaxUint16 {
			entry.Port = uint16(peer.Addr.Port)
		}
		peerList = append(peerList, entry)
	}

	return peerList
}

func (s *IPSCServer) peerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

func (s *IPSCServer) defaultModeByte() byte {
	const (
		peerOperational = 0b01000000
		peerDigital     = 0b00100000
		ts1On           = 0b00001000
		ts2On           = 0b00000010
	)
	return peerOperational | peerDigital | ts1On | ts2On
}

func (s *IPSCServer) defaultFlagsBytes() [4]byte {
	var flags ipscwire.Flags
	flags.Set(ipscwire.FlagAuth, true)
	flags.Set(ipscwire.FlagData, true)
	flags.Set(ipscwire.FlagVoice, true)
	flags.Set(ipscwire.FlagMasterPeer, true)
	return flags
}

// parsePeerID returns the sender ID of an IPSC packet, octets 1 to 4.
func parsePeerID(data []byte) (uint32, error) {
	raw, err := ipscwire.DecodeRaw(data)
	if err != nil {
		return 0, fmt.Errorf("packet too short for peer ID: %w", err)
	}
	return raw.ID, nil
}

func cloneUDPAddr(addr *net.UDPAddr) *net.UDPAddr {
	if addr == nil {
		return nil
	}
	cloned := &net.UDPAddr{Port: addr.Port, Zone: addr.Zone}
	if addr.IP != nil {
		cloned.IP = append([]byte(nil), addr.IP...)
	}
	return cloned
}

func (s *IPSCServer) getPeerAuthKey(peerID uint32) *[20]byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[peerID]
	if !ok {
		return nil
	}
	return peer.AuthKey
}

func (s *IPSCServer) lookupAndCachePeerAuthKey(peerID uint32) (*[20]byte, error) {
	authKey := s.getPeerAuthKey(peerID)
	if authKey != nil {
		return authKey, nil
	}

	authKey, err := s.lookupPeerAuthKey(peerID)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	peer, ok := s.peers[peerID]
	if !ok {
		peer = &Peer{ID: peerID}
		s.peers[peerID] = peer
	}
	peer.AuthKey = authKey
	s.mu.Unlock()

	return authKey, nil
}

// authWithKey reports whether the last ipscauth.TrailerLen octets of data
// are its authentication trailer for key.
func authWithKey(data []byte, key *[20]byte) bool {
	_, ok := ipscauth.NewKey(*key).Verify(data)
	return ok
}

// consumeHubPackets reads routed packets from the hub and sends them to all IPSC peers.
func (s *IPSCServer) consumeHubPackets() {
	defer s.wg.Done()
	for rp := range s.hubHandle.Packets {
		s.SendPacket(rp.Packet)
	}
}

func (s *IPSCServer) sendPacket(packet *Packet, addr *net.UDPAddr, authKey *[20]byte) error {
	if authKey != nil {
		packet.data = ipscauth.NewKey(*authKey).Sign(packet.data)
	}

	n, err := s.udp.WriteToUDP(packet.data, addr)
	if err != nil {
		return fmt.Errorf("error sending packet: %w", err)
	}
	if n != len(packet.data) {
		return fmt.Errorf("error sending packet: only sent %d of %d bytes", n, len(packet.data))
	}
	return nil
}

func (s *IPSCServer) SendPacket(pkt models.Packet) {
	ipscFrames := s.translator.TranslateToIPSC(pkt)
	for _, frame := range ipscFrames {
		s.sendPacketInternal(frame)
	}
}

func (s *IPSCServer) sendPacketInternal(data []byte) {
	if s.stopped.Load() {
		return
	}
	s.mu.RLock()
	peers := make([]*Peer, 0, len(s.peers))
	for _, peer := range s.peers {
		if peer.Addr != nil && peer.RegistrationStatus {
			peers = append(peers, peer)
		}
	}
	s.mu.RUnlock()

	for _, peer := range peers {
		s.pacePeer(peer.ID)
		packetData := make([]byte, len(data))
		copy(packetData, data)
		packet := &Packet{data: packetData}
		slog.Debug("IPSC burst sending", "peer", peer.Addr, "length", len(packet.data))
		if err := s.sendPacket(packet, peer.Addr, peer.AuthKey); err != nil {
			slog.Warn("failed sending IPSC user packet", "peer", peer.Addr, "error", err)
		}
	}
}

func (s *IPSCServer) pacePeer(peerID uint32) {
	const burstInterval = 30 * time.Millisecond

	s.mu.Lock()
	last := s.lastSend[peerID]
	now := time.Now()
	if !last.IsZero() {
		elapsed := now.Sub(last)
		if elapsed < burstInterval {
			s.mu.Unlock()
			time.Sleep(burstInterval - elapsed)
			s.mu.Lock()
		}
	}
	s.lastSend[peerID] = time.Now()
	s.mu.Unlock()
}
