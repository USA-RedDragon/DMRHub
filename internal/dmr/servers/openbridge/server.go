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
	"crypto/hmac"
	"crypto/sha1" //#nosec G505 -- False positive, used for a protocol
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"slices"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

const packetLength = 73
const largestMessageSize = 73
const bufferSize = 1000000 // 1MB

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
	wg        sync.WaitGroup
	done      chan struct{}
	stopOnce  sync.Once
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
		Buffer:        make([]byte, largestMessageSize),
		SocketAddress: socketAddress,
		hub:           hub,
		Server:        server,
		DB:            db,
		pubsub:        pubsub,
		Tracer:        otel.Tracer("dmr-openbridge-server"),
		done:          make(chan struct{}),
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

	s.wg.Add(3)
	go s.listen(ctx)
	go s.consumeHubPackets(ctx)

	go func() {
		defer s.wg.Done()
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			slog.Debug("Read from UDP", "length", length, "remoteaddr", remoteaddr, "err", err)
			if err != nil {
				// When the socket is closed during shutdown, stop the loop quietly.
				if errors.Is(err, net.ErrClosed) {
					return
				}
				slog.Error("Error reading from UDP Socket, swallowing error", "error", err)
				continue
			}
			// Copy the buffer data since s.Buffer will be reused for the next read
			data := make([]byte, length)
			copy(data, s.Buffer[:length])
			go func() {
				p := models.RawDMRPacket{
					Data:       data,
					RemoteIP:   remoteaddr.IP.String(),
					RemotePort: remoteaddr.Port,
				}
				packedBytes, err := p.MarshalMsg(nil)
				if err != nil {
					slog.Error("Error marshalling packet", "error", err)
					return
				}
				if err := s.pubsub.Publish("openbridge:incoming", packedBytes); err != nil {
					slog.Error("Error publishing packet to openbridge:incoming", "error", err)
					return
				}
			}()
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

func (s *Server) listen(ctx context.Context) {
	defer s.wg.Done()
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.listen")
	defer span.End()

	subscription := s.pubsub.Subscribe("openbridge:incoming")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub", "error", err)
		}
	}()
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-subscription.Channel():
			if !ok {
				return
			}
			var packet models.RawDMRPacket
			_, err := packet.UnmarshalMsg(msg)
			if err != nil {
				slog.Error("Error unmarshalling packet", "error", err)
				continue
			}
			go s.handlePacket(ctx, &net.UDPAddr{
				IP:   net.ParseIP(packet.RemoteIP),
				Port: packet.RemotePort,
			}, packet.Data)
		}
	}
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

	slog.Debug("Sending OpenBridge packet", "packet", packet.String(), "peerID", peerID)
	peer, err := models.FindPeerByID(s.DB, peerID)
	if err != nil {
		slog.Error("Error getting peer from DB", "peerID", peerID, "error", err)
		return
	}
	// OpenBridge is always TS1
	packet.Slot = false
	packet.Repeater = peerID
	encodedPacket := packet.Encode()[:dmrconst.MMDVMPacketLength]

	// Compute HMAC-SHA1 and append to outbound packet
	h := hmac.New(sha1.New, []byte(peer.Password))
	_, err = h.Write(encodedPacket)
	if err != nil {
		slog.Error("Error computing HMAC for outbound OpenBridge packet", "error", err)
		return
	}
	outbound := slices.Concat(encodedPacket, h.Sum(nil))

	_, err = s.Server.WriteToUDP(outbound, &net.UDPAddr{
		IP:   net.ParseIP(peer.IP),
		Port: peer.Port,
	})
	if err != nil {
		slog.Error("Error sending packet", "error", err)
	}
}

func (s *Server) validateHMAC(ctx context.Context, packetBytes []byte, hmacBytes []byte, peer models.Peer) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Server.validateHMAC")
	defer span.End()

	h := hmac.New(sha1.New, []byte(peer.Password))
	_, err := h.Write(packetBytes)
	if err != nil {
		slog.Error("Error hashing OpenBridge packet", "error", err)
		return false
	}
	if !hmac.Equal(h.Sum(nil), hmacBytes) {
		slog.Error("Invalid OpenBridge HMAC")
		return false
	}
	return true
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	const signatureLength = 4

	if len(data) != packetLength {
		slog.Error("Invalid OpenBridge packet length", "length", len(data), "expected", packetLength)
		return
	}

	if dmrconst.Command(data[:signatureLength]) != dmrconst.CommandDMRD {
		slog.Error("Unknown command", "command", string(data[:signatureLength]))
		return
	}

	packetBytes := data[:dmrconst.MMDVMPacketLength]
	hmacBytes := data[dmrconst.MMDVMPacketLength:packetLength]

	packet, ok := models.UnpackPacket(packetBytes)
	if !ok {
		slog.Error("Invalid OpenBridge packet")
		return
	}

	slog.Debug("OpenBridge packet received", "packet", packet.String())

	if packet.Slot {
		// Drop TS2 packets on OpenBridge
		slog.Debug("Dropping TS2 packet from OpenBridge")
		return
	}

	peerIDBytes := data[11:15]
	peerID := uint(binary.BigEndian.Uint32(peerIDBytes))
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

	if !s.validateHMAC(ctx, packetBytes, hmacBytes, peer) {
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
