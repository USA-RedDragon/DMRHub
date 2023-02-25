// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023 Jacob McSwain
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
	"net"
	"os"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
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

	DB    *gorm.DB
	Redis *servers.RedisClient

	CallTracker *calltracker.CallTracker
}

// MakeServer creates a new DMR server.
func MakeServer(db *gorm.DB, redisClient *servers.RedisClient, callTracker *calltracker.CallTracker) Server {
	return Server{
		Buffer: make([]byte, largestMessageSize),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(config.GetConfig().ListenAddr),
			Port: config.GetConfig().OpenBridgePort,
		},
		DB:          db,
		Redis:       redisClient,
		CallTracker: callTracker,
		Tracer:      otel.Tracer("dmr-openbridge-server"),
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()

	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		logging.Errorf("Error opening UDP Socket: %v", err)
		os.Exit(1)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		logging.Errorf("Error setting UDP Socket read buffer: %v", err)
		os.Exit(1)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		logging.Errorf("Error setting UDP Socket write buffer: %v", err)
		os.Exit(1)
	}

	s.Server = server

	logging.Logf("OpenBridge Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)

	go s.listen(ctx)
	go s.subcribeOutgoing(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if config.GetConfig().Debug {
				logging.Logf("Read a message from %v\n", remoteaddr)
			}
			if err != nil {
				logging.Errorf("Error reading from UDP Socket, Swallowing Error: %v", err)
				continue
			}
			go func() {
				p := models.RawDMRPacket{
					Data:       s.Buffer[:length],
					RemoteIP:   remoteaddr.IP.String(),
					RemotePort: remoteaddr.Port,
				}
				packedBytes, err := p.MarshalMsg(nil)
				if err != nil {
					logging.Errorf("Error marshalling packet", err)
					return
				}
				s.Redis.Redis.Publish(ctx, "openbridge:incoming", packedBytes)
			}()
		}
	}()
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) {
}

func (s *Server) listen(ctx context.Context) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.listen")
	defer span.End()

	pubsub := s.Redis.Redis.Subscribe(ctx, "openbridge:incoming")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			logging.Errorf("Error unmarshalling packet", err)
			continue
		}
		go s.handlePacket(ctx, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		}, packet.Data)
	}
}

func (s *Server) subcribeOutgoing(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "openbridge:outgoing")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		packet, ok := models.UnpackPacket([]byte(msg.Payload))
		if !ok {
			logging.Errorf("Error unpacking packet")
			continue
		}
		peer, err := s.Redis.GetPeer(ctx, packet.Repeater)
		if err != nil {
			logging.Errorf("Error getting peer %d from redis", packet.Repeater)
			continue
		}
		// OpenBridge is always TS1
		packet.Slot = false
		_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(peer.IP),
			Port: peer.Port,
		})
		if err != nil {
			logging.Errorf("Error sending packet", err)
		}
	}
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		logging.Errorf("Invalid packet type: %s", packet.Signature)
		return
	}

	if config.GetConfig().Debug {
		logging.Logf("Sending Packet: %s\n", packet.String())
		logging.Logf("Sending DMR packet to Repeater ID: %d", repeaterIDBytes)
	}
	repeater, err := s.Redis.GetPeer(ctx, repeaterIDBytes)
	if err != nil {
		logging.Errorf("Error getting repeater from Redis: %v", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       packet.Encode(),
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		logging.Errorf("Error marshalling packet: %v", err)
		return
	}
	s.Redis.Redis.Publish(ctx, "openbridge:outgoing", packedBytes)
}

func (s *Server) validateHMAC(ctx context.Context, packetBytes []byte, hmacBytes []byte, peer models.Peer) bool {
	_, span := otel.Tracer("DMRHub").Start(ctx, "Server.validateHMAC")
	defer span.End()

	h := hmac.New(sha1.New, []byte(peer.Password))
	_, err := h.Write(packetBytes)
	if err != nil {
		logging.Errorf("Error hashing OpenBridge packet: %s", err)
		return false
	}
	if !hmac.Equal(h.Sum(nil), hmacBytes) {
		logging.Error("Invalid OpenBridge HMAC")
		return false
	}
	return true
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()

	const signatureLength = 4

	if len(data) != packetLength {
		logging.Errorf("Invalid OpenBridge packet length: %d", len(data))
		return
	}

	if dmrconst.Command(data[:signatureLength]) != dmrconst.CommandDMRD {
		logging.Errorf("Unknown command: %s", data[:signatureLength])
		return
	}

	packetBytes := data[:dmrconst.HBRPPacketLength]
	hmacBytes := data[dmrconst.HBRPPacketLength:packetLength]

	packet, ok := models.UnpackPacket(packetBytes)
	if !ok {
		logging.Error("Invalid OpenBridge packet")
		return
	}

	if config.GetConfig().Debug {
		logging.Logf("DMRD packet: %s", packet.String())
	}

	if packet.Slot {
		// Drop TS2 packets on OpenBridge
		logging.Log("Dropping TS2 packet from OpenBridge")
		return
	}

	peerIDBytes := data[11:15]
	peerID := uint(binary.BigEndian.Uint32(peerIDBytes))
	if config.GetConfig().Debug {
		logging.Logf("DMR Data from Peer ID: %d", peerID)
	}

	if !models.PeerIDExists(s.DB, peerID) {
		logging.Errorf("Unknown peer ID: %d", peerID)
		return
	}

	peer := models.FindPeerByID(s.DB, peerID)

	if !s.validateHMAC(ctx, packetBytes, hmacBytes, peer) {
		logging.Error("Invalid OpenBridge HMAC")
		return
	}

	if !rules.PeerShouldIngress(s.DB, &peer, &packet) {
		return
	}

	// We need to send this packet to all peers except the one that sent it
	peers := models.ListPeers(s.DB)
	for _, p := range peers {
		if p.ID == peerID {
			continue
		}
		if rules.PeerShouldEgress(s.DB, &p, &packet) {
			s.sendPacket(ctx, p.ID, packet)
		}
	}

	// s.TrackCall(ctx, pkt, true)
	// TODO: And if this packet goes to a destination we are aware of, send it there too
}

func (s *Server) TrackCall(ctx context.Context, packet models.Packet, isVoice bool) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.TrackCall")
	defer span.End()

	// Don't call track unlink
	if packet.Dst != 4000 && isVoice {
		if !s.CallTracker.IsCallActive(ctx, packet) {
			s.CallTracker.StartCall(ctx, packet)
		}
		if s.CallTracker.IsCallActive(ctx, packet) {
			s.CallTracker.ProcessCallPacket(ctx, packet)
			if packet.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(packet.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
				s.CallTracker.EndCall(ctx, packet)
			}
		}
	}
}
