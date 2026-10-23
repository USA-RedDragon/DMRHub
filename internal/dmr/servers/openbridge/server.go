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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdauth "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdauth"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const packetLength = 73
const bufferSize = 1000000 // 1MB

const (
	ingressQueueSize = 500
	quenchTTL        = 30 * time.Second
	quenchPruneSize  = 1024
)

// OpenBridge is the same as HBRP, but with a single packet type.
type Server struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Tracer        trace.Tracer

	DB     *gorm.DB
	pubsub pubsub.PubSub
	hub    *hub.Hub

	hubHandle *hub.ServerHandle
	ingress   *streamorder.Queue
	wg        sync.WaitGroup
	done      chan struct{}
	stopOnce  sync.Once

	bridgesMu sync.Mutex
	bridges   map[uint]*bridge
	epoch     time.Time

	quenched *xsync.Map[quench, time.Time]
}

// quench is a stream that a peer asked with a BCSQ not to get.
type quench struct {
	peer   uint
	stream uint
}

// bridge is the OpenBridge link of one peer, for the passphrase it was made
// with.
type bridge struct {
	mu       sync.Mutex
	password string
	b        *dmrdsession.Bridge
	key      *dmrdauth.BridgeKey
}

func (s *Server) bridgeFor(peer models.Peer) (*bridge, error) {
	s.bridgesMu.Lock()
	defer s.bridgesMu.Unlock()
	if b, ok := s.bridges[peer.ID]; ok && b.password == peer.Password {
		return b, nil
	}
	sb, err := dmrdsession.NewBridge(&dmrdsession.BridgeConfig{Key: []byte(peer.Password)})
	if err != nil {
		return nil, fmt.Errorf("failed to make OpenBridge link: %w", err)
	}
	if s.bridges == nil {
		s.bridges = map[uint]*bridge{}
	}
	b := &bridge{password: peer.Password, b: sb, key: dmrdauth.NewBridgeKey([]byte(peer.Password))}
	s.bridges[peer.ID] = b
	return b, nil
}

// verified reports whether data is a DMRD or DMRE packet with a good
// signature.
func (b *bridge) verified(now time.Duration, data []byte) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	for _, o := range b.b.Packet(now, data, buf[:0]) {
		if o.Kind == dmrdsession.OutputData && (o.Command == dmrd.CommandDMRD || o.Command == dmrd.CommandDMRE) {
			return true
		}
	}
	return false
}

// verifiedControl reports whether data is a control packet with a good
// signature.
func (b *bridge) verifiedControl(data []byte) bool {
	_, ok := b.key.Verify(data)
	return ok
}

func (b *bridge) sign(p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.b.SignInPlace(p); err != nil {
		return fmt.Errorf("failed to sign OpenBridge packet: %w", err)
	}
	return nil
}

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, hub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub) (Server, error) {
	socketAddress := net.UDPAddr{
		IP:   net.ParseIP(config.DMR.OpenBridge.Bind),
		Port: config.DMR.OpenBridge.Port,
	}
	server, err := net.ListenUDP("udp", &socketAddress)
	if err != nil {
		return Server{}, fmt.Errorf("error opening UDP Socket: %w", err)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		return Server{}, fmt.Errorf("error setting UDP Socket read buffer: %w", err)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		return Server{}, fmt.Errorf("error setting UDP Socket write buffer: %w", err)
	}

	return Server{
		Buffer:        make([]byte, dmrdsession.MaxPacket+1),
		SocketAddress: socketAddress,
		hub:           hub,
		Server:        server,
		DB:            db,
		pubsub:        pubsub,
		Tracer:        otel.Tracer("dmr-openbridge-server"),
		done:          make(chan struct{}),
		epoch:         time.Now(),
		quenched:      xsync.NewMap[quench, time.Time](),
	}, nil
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()

	// Register with the hub as a peer server
	s.hubHandle = s.hub.RegisterServer(hub.ServerConfig{
		Name: "openbridge",
		Role: hub.RolePeer,
	})

	slog.Info("OpenBridge Server listening", "address", s.SocketAddress.IP.String(), "port", s.SocketAddress.Port)

	s.ingress = streamorder.New(ingressQueueSize, s.done)

	s.wg.Add(2)
	go s.consumeHubPackets(ctx)

	go func() {
		defer s.wg.Done()
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if err != nil {
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Error("Error reading from UDP Socket, swallowing error", "error", err)
				continue
			}
			data := make([]byte, length)
			copy(data, s.Buffer[:length])
			stream := streamOf(data)
			if !s.ingress.Do(ctx, stream, func() { s.handlePacket(ctx, remoteaddr, data) }) {
				slog.Warn("Dropping OpenBridge packet of a backed-up stream", "stream", stream)
			}
		}
	}()

	return nil
}

// Stop stops the DMR server.
func (s *Server) Stop(_ context.Context) error {
	s.stopOnce.Do(func() {
		slog.Info("Stopping OpenBridge server")

		close(s.done)
		s.hub.UnregisterServer("openbridge")

		if err := s.Server.Close(); err != nil {
			slog.Error("Error closing OpenBridge UDP socket", "error", err)
		}
	})

	s.wg.Wait()
	return nil
}

// consumeHubPackets reads routed packets from the hub and sends them to OpenBridge peers.
func (s *Server) consumeHubPackets(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case rp, ok := <-s.hubHandle.Packets:
			if !ok {
				return
			}
			// rp.RepeaterID is the peer ID for peer-role servers
			s.sendPacketToPeer(ctx, rp.RepeaterID, rp.Packet)
		}
	}
}

// sendPacketToPeer sends a packet to a specific OpenBridge peer.
func (s *Server) sendPacketToPeer(_ context.Context, peerID uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type", "packetType", packet.Signature)
		return
	}

	if s.isQuenched(peerID, packet.StreamID) {
		return
	}
	slog.Debug("Sending OpenBridge packet", "packet", packet.String(), "peerID", peerID)
	peer, err := models.FindPeerByID(s.DB, peerID)
	if err != nil {
		slog.Error("Error getting peer from DB", "peerID", peerID, "error", err)
		return
	}
	// OpenBridge is always TS1
	packet.Slot = false
	packet.Repeater = peerID
	bridge := dmrd.Bridge{Version: 1, Data: packet.Data()}
	bridge.Data.Size = dmrconst.MMDVMPacketLength
	outbound, err := bridge.AppendBinary(make([]byte, 0, packetLength))
	if err != nil {
		slog.Error("Error encoding OpenBridge packet", "error", err)
		return
	}
	b, err := s.bridgeFor(peer)
	if err != nil {
		slog.Error("Error making OpenBridge link", "peerID", peerID, "error", err)
		return
	}
	if err := b.sign(outbound); err != nil {
		slog.Error("Error signing outbound OpenBridge packet", "error", err)
		return
	}

	_, err = s.Server.WriteToUDP(outbound, &net.UDPAddr{
		IP:   net.ParseIP(peer.IP),
		Port: peer.Port,
	})
	if err != nil {
		slog.Error("Error sending packet", "error", err)
	}
}

func streamOf(data []byte) uint {
	b, err := dmrd.DecodeBridge(data)
	if err != nil {
		return 0
	}
	return uint(b.Data.Stream)
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	bridge, err := dmrd.DecodeBridge(data)
	if err != nil {
		if control, cerr := dmrd.DecodeBridgeControl(data); cerr == nil {
			s.handleControl(remoteAddr, data, control)
			return
		}
		slog.Debug("Dropping a packet that is not OpenBridge", "length", len(data), "error", err)
		return
	}

	packet := models.PacketFromData(&bridge.Data, false)

	slog.Debug("OpenBridge packet received", "packet", packet.String())

	if packet.Slot {
		// Drop TS2 packets on OpenBridge
		slog.Debug("Dropping TS2 packet from OpenBridge")
		return
	}

	peerID := uint(bridge.Data.Repeater)
	slog.Debug("OpenBridge packet peer ID", "peerID", peerID)

	exists, err := models.PeerIDExists(s.DB, peerID)
	if err != nil {
		slog.Error("Failed to check peer existence", "peerID", peerID, "error", err)
		return
	}
	if !exists {
		slog.Error("Unknown peer ID", "peerID", peerID)
		return
	}

	peer, err := models.FindPeerByID(s.DB, peerID)
	if err != nil {
		slog.Error("Failed to find peer", "peerID", peerID, "error", err)
		return
	}

	b, err := s.bridgeFor(peer)
	if err != nil {
		slog.Error("Error making OpenBridge link", "peerID", peerID, "error", err)
		return
	}
	if !b.verified(time.Since(s.epoch), data) {
		slog.Error("Invalid OpenBridge HMAC", "peerID", peerID)
		return
	}

	// Warn if the source IP doesn't match the configured peer IP
	if peer.IP != "" && remoteAddr != nil && remoteAddr.IP.String() != peer.IP {
		slog.Warn("OpenBridge packet source IP does not match configured peer IP",
			"peerID", peerID,
			"configuredIP", peer.IP,
			"sourceIP", remoteAddr.IP.String(),
		)
	}

	should, err := rules.PeerShouldIngress(s.DB, &peer, &packet)
	if err != nil {
		slog.Error("Failed to check peer ingress rules", "peerID", peerID, "error", err)
		return
	}
	if !should {
		return
	}

	// We need to send this packet to all peers except the one that sent it
	peers, err := models.ListPeers(s.DB)
	if err != nil {
		slog.Error("Failed to list peers", "error", err)
		return
	}
	for _, p := range peers {
		if p.ID == peerID {
			continue
		}
		should, err := rules.PeerShouldEgress(s.DB, p, &packet)
		if err != nil {
			slog.Error("Failed to check peer egress rules", "peerID", p.ID, "error", err)
			continue
		}
		if should {
			s.sendPacketToPeer(ctx, p.ID, packet)
		}
	}

	// Route to local repeaters via Hub
	s.hub.RoutePacket(ctx, packet, "openbridge")

	// Publish peer activity event for WebSocket consumers
	peerEvent := fmt.Sprintf(`{"peer_id":%d,"event":"active","src":%d,"dst":%d}`, peerID, packet.Src, packet.Dst)
	if err := s.pubsub.Publish("hub:events:peers", []byte(peerEvent)); err != nil {
		slog.Error("Error publishing peer event", "error", err)
	}
}

// handleControl takes a control packet of FreeDMR's enhanced OpenBridge.
// It carries no peer ID, so the peer is the one at the source address
// whose key signed it. A BCSQ asks not to get a stream; BCKA, BCST and BCVE
// change nothing here.
func (s *Server) handleControl(remoteAddr *net.UDPAddr, data []byte, control dmrd.BridgeControl) {
	peers, err := models.ListPeers(s.DB)
	if err != nil {
		slog.Error("Failed to list peers", "error", err)
		return
	}
	for _, p := range peers {
		if remoteAddr == nil || p.IP != remoteAddr.IP.String() || (p.Port != 0 && p.Port != remoteAddr.Port) {
			continue
		}
		b, err := s.bridgeFor(p)
		if err != nil || !b.verifiedControl(data) {
			continue
		}
		if control.Command == dmrd.CommandBCSQ {
			slog.Debug("OpenBridge peer quenched a stream", "peerID", p.ID, "talkgroup", control.TalkGroup, "stream", control.Stream)
			s.quenchStream(p.ID, uint(control.Stream))
			return
		}
		slog.Debug("OpenBridge control packet", "peerID", p.ID, "command", string(control.Command), "version", control.Version)
		return
	}
	slog.Debug("Dropping an OpenBridge control packet of no known peer", "remote", remoteAddr)
}

func (s *Server) quenchStream(peer, stream uint) {
	now := time.Now()
	s.quenched.Store(quench{peer: peer, stream: stream}, now)
	if s.quenched.Size() > quenchPruneSize {
		s.quenched.Range(func(q quench, at time.Time) bool {
			if now.Sub(at) > quenchTTL {
				s.quenched.Delete(q)
			}
			return true
		})
	}
}

func (s *Server) isQuenched(peer, stream uint) bool {
	at, ok := s.quenched.Load(quench{peer: peer, stream: stream})
	if !ok {
		return false
	}
	if time.Since(at) > quenchTTL {
		s.quenched.Delete(quench{peer: peer, stream: stream})
		return false
	}
	return true
}
