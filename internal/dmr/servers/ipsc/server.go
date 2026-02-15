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
	"crypto/hmac"
	"crypto/sha1" //nolint:gosec
	"encoding/binary"
	"encoding/hex"
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
	AuthKey            []byte // cached HMAC key for this peer
}

type PacketType byte

const (
	PacketType_GroupVoice            PacketType = 0x80
	PacketType_PrivateVoice          PacketType = 0x81
	PacketType_GroupData             PacketType = 0x83
	PacketType_PrivateData           PacketType = 0x84
	PacketType_RepeaterWakeUp        PacketType = 0x85
	PacketType_MasterRegisterRequest PacketType = 0x90
	PacketType_MasterRegisterReply   PacketType = 0x91
	PacketType_PeerListRequest       PacketType = 0x92
	PacketType_PeerListReply         PacketType = 0x93
	PacketType_MasterAliveRequest    PacketType = 0x96
	PacketType_MasterAliveReply      PacketType = 0x97
	PacketType_DeregistrationRequest PacketType = 0x9A
	PacketType_DeregistrationReply   PacketType = 0x9B
)

var (
	//nolint:gochecknoglobals
	ipscVersion = []byte{0x04, 0x02, 0x04, 0x01}
)

var ErrPacketIgnored = errors.New("packet ignored")

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

// decodeAuthKey decodes a hex auth key string into raw bytes.
// DMRlink left-pads the hex key to 40 characters (20 bytes) with zeros.
func decodeAuthKey(hexKey string) []byte {
	for len(hexKey) < 40 {
		hexKey = "0" + hexKey
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil {
		slog.Error("failed to decode IPSC auth key as hex, using raw string", "error", err)
		return []byte(hexKey)
	}
	return key
}

// lookupPeerAuthKey looks up a repeater's auth key from the database.
func (s *IPSCServer) lookupPeerAuthKey(peerID uint32) ([]byte, error) {
	if s.db == nil {
		return nil, fmt.Errorf("no database configured")
	}
	var repeater models.Repeater
	err := s.db.Where("id = ? AND type = ?", peerID, models.RepeaterTypeIPSC).First(&repeater).Error
	if err != nil {
		return nil, fmt.Errorf("repeater %d not found: %w", peerID, err)
	}
	if repeater.Password == "" {
		return nil, nil
	}
	return decodeAuthKey(repeater.Password), nil
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

	if len(data) <= 10 {
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
	data = data[:len(data)-10] // Remove the hash from the data

	s.mu.RLock()
	peer, ok := s.peers[peerID]
	registered := ok && peer.RegistrationStatus
	s.mu.RUnlock()

	if !registered && PacketType(packetType) != PacketType_MasterRegisterRequest {
		// The peer passed HMAC authentication but isn't registered on this
		// server instance.  This commonly happens during rolling restarts:
		// the peer was connected to another pod that terminated, and is now
		// sending keepalives/data to this pod.
		//
		// Rather than dropping the packet and sending a DeregistrationRequest
		// (which the peer may interpret as "master is leaving" and enter a
		// slow retry loop), auto-register the peer so it seamlessly
		// transitions to this instance.
		if PacketType(packetType) == PacketType_DeregistrationRequest {
			// Peer is asking to leave — nothing to auto-register.
			return nil, ErrPacketIgnored
		}
		slog.Info("auto-registering authenticated peer", "peerID", peerID, "peer", addr, "packetType", packetType)
		s.upsertPeer(ctx, peerID, addr, s.defaultModeByte(), s.defaultFlagsBytes())
	}

	switch PacketType(packetType) {
	case PacketType_GroupVoice, PacketType_PrivateVoice, PacketType_GroupData, PacketType_PrivateData:
		if err := s.handleUserPacket(PacketType(packetType), data, addr); err != nil {
			return nil, err
		}
	case PacketType_RepeaterWakeUp:
		if err := s.handleRepeaterWakeUp(data, addr); err != nil {
			return nil, err
		}
	case PacketType_MasterRegisterRequest:
		if err := s.handleMasterRegisterRequest(ctx, data, addr); err != nil {
			return nil, err
		}
	case PacketType_MasterAliveRequest:
		if err := s.handleMasterAliveRequest(data, addr); err != nil {
			return nil, err
		}
	case PacketType_PeerListRequest:
		if err := s.handlePeerListRequest(data, addr); err != nil {
			return nil, err
		}
	case PacketType_DeregistrationRequest:
		s.handleDeregistrationRequest(ctx, peerID)
		return nil, ErrPacketIgnored
	case PacketType_MasterRegisterReply, PacketType_PeerListReply, PacketType_MasterAliveReply, PacketType_DeregistrationReply:
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
	if len(data) >= 10 {
		mode = data[5]
		copy(flags[:], data[6:10])
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

func (s *IPSCServer) handleUserPacket(packetType PacketType, data []byte, addr *net.UDPAddr) error {
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

func (s *IPSCServer) upsertPeer(ctx context.Context, peerID uint32, addr *net.UDPAddr, mode byte, flags [4]byte) []byte {
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

func (s *IPSCServer) markPeerAlive(peerID uint32, addr *net.UDPAddr) []byte {
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
	packet := make([]byte, 0, 1+4+5+2+4)
	packet = append(packet, byte(PacketType_MasterRegisterReply))
	packet = append(packet, s.localIDBytes()...)
	packet = append(packet, s.defaultModeByte())
	flags := s.defaultFlagsBytes()
	packet = append(packet, flags[:]...)

	numPeers := s.peerCount()
	if numPeers > math.MaxUint16 {
		numPeers = math.MaxUint16
	}
	packet = append(packet, uint16ToBytes(uint16(numPeers))...) //nolint:gosec // Bounds checked
	packet = append(packet, ipscVersion...)
	return packet
}

func (s *IPSCServer) buildMasterAliveReply() []byte {
	packet := make([]byte, 0, 1+4+5+4)
	packet = append(packet, byte(PacketType_MasterAliveReply))
	packet = append(packet, s.localIDBytes()...)
	packet = append(packet, s.defaultModeByte())
	flags := s.defaultFlagsBytes()
	packet = append(packet, flags[:]...)
	packet = append(packet, ipscVersion...)
	return packet
}

func (s *IPSCServer) buildPeerListReply() []byte {
	peerList := s.buildPeerList()
	packet := make([]byte, 0, 1+4+2+len(peerList))
	packet = append(packet, byte(PacketType_PeerListReply))
	packet = append(packet, s.localIDBytes()...)
	if len(peerList) > math.MaxUint16 {
		packet = append(packet, uint16ToBytes(math.MaxUint16)...)
	} else {
		packet = append(packet, uint16ToBytes(uint16(len(peerList)))...) //nolint:gosec // Bounds checked
	}
	packet = append(packet, peerList...)
	return packet
}

func (s *IPSCServer) buildPeerList() []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if len(s.peers) == 0 {
		return nil
	}

	peerList := make([]byte, 0, len(s.peers)*11)
	for _, peer := range s.peers {
		if peer.Addr == nil || peer.Addr.IP == nil {
			continue
		} //nolint:gosec
		peerList = append(peerList, uint32ToBytes(peer.ID)...)
		peerList = append(peerList, peer.Addr.IP.To4()...)
		peerPort := peer.Addr.Port
		if peerPort < 0 || peerPort > 65535 {
			peerPort = 0
		}
		peerList = append(peerList, uint16ToBytes(uint16(peerPort))...) //nolint:gosec // Bounds checked
		peerList = append(peerList, peer.Mode)
	}

	return peerList
}

func (s *IPSCServer) peerCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.peers)
}

func (s *IPSCServer) localIDBytes() []byte {
	return uint32ToBytes(s.localID)
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
	flags := [4]byte{}
	flags[2] = 0x00
	flags[3] = 0x0D
	flags[3] |= 0x10
	return flags
}

func parsePeerID(data []byte) (uint32, error) {
	if len(data) < 5 {
		return 0, fmt.Errorf("packet too short for peer ID")
	}
	return binary.BigEndian.Uint32(data[1:5]), nil
}

func uint16ToBytes(value uint16) []byte {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, value)
	return buf
}

func uint32ToBytes(value uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, value)
	return buf
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

func (s *IPSCServer) getPeerAuthKey(peerID uint32) []byte {
	s.mu.RLock()
	defer s.mu.RUnlock()
	peer, ok := s.peers[peerID]
	if !ok {
		return nil
	}
	return peer.AuthKey
}

func (s *IPSCServer) lookupAndCachePeerAuthKey(peerID uint32) ([]byte, error) {
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

func authWithKey(data []byte, key []byte) bool {
	const truncatedHashLen = 10
	// Last 10 bytes are the truncated HMAC-SHA1
	payload := data[:len(data)-truncatedHashLen]
	hash := data[len(data)-truncatedHashLen:]

	mac := hmac.New(sha1.New, key)
	mac.Write(payload)
	var sumBuf [sha1.Size]byte
	expected := mac.Sum(sumBuf[:0])[:truncatedHashLen]

	return hmac.Equal(hash, expected)
}

// consumeHubPackets reads routed packets from the hub and sends them to all IPSC peers.
func (s *IPSCServer) consumeHubPackets() {
	defer s.wg.Done()
	for rp := range s.hubHandle.Packets {
		s.SendPacket(rp.Packet)
	}
}

func (s *IPSCServer) sendPacket(packet *Packet, addr *net.UDPAddr, authKey []byte) error {
	if authKey != nil {
		hash := hmac.New(sha1.New, authKey)
		hash.Write(packet.data)
		hashSum := hash.Sum(nil)[:10]
		packet.data = append(packet.data, hashSum...)
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
		ReturnBuffer(frame)
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
