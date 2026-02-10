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
	"fmt"
	"log/slog"
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
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

	DB       *gorm.DB
	kvClient *servers.KVClient
	pubsub   pubsub.PubSub

	CallTracker *calltracker.CallTracker
}

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, callTracker *calltracker.CallTracker) Server {
	return Server{
		Buffer: make([]byte, largestMessageSize),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(config.DMR.OpenBridge.Bind),
			Port: config.DMR.OpenBridge.Port,
		},
		DB:          db,
		pubsub:      pubsub,
		kvClient:    servers.MakeKVClient(kv),
		CallTracker: callTracker,
		Tracer:      otel.Tracer("dmr-openbridge-server"),
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()

	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		return fmt.Errorf("error opening UDP Socket: %w", err)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		return fmt.Errorf("error setting UDP Socket read buffer: %w", err)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		return fmt.Errorf("error setting UDP Socket write buffer: %w", err)
	}

	s.Server = server

	slog.Info("OpenBridge Server listening", "address", s.SocketAddress.IP.String(), "port", s.SocketAddress.Port)

	go s.listen(ctx)
	go s.subcribeOutgoing(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			slog.Debug("Read from UDP", "length", length, "remoteaddr", remoteaddr, "err", err)
			if err != nil {
				slog.Error("Error reading from UDP Socket, swallowing error", "error", err)
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
func (s *Server) Stop(_ context.Context) {
}

func (s *Server) listen(ctx context.Context) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.listen")
	defer span.End()

	subscription := s.pubsub.Subscribe("openbridge:incoming")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub", "error", err)
		}
	}()
	for msg := range subscription.Channel() {
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

func (s *Server) subcribeOutgoing(ctx context.Context) {
	subscription := s.pubsub.Subscribe("openbridge:outgoing")
	defer func() {
		err := subscription.Close()
		if err != nil {
			slog.Error("Error closing pubsub", "error", err)
		}
	}()
	for msg := range subscription.Channel() {
		packet, ok := models.UnpackPacket(msg)
		if !ok {
			slog.Error("Error unpacking packet")
			continue
		}
		peer, err := s.kvClient.GetPeer(ctx, packet.Repeater)
		if err != nil {
			slog.Error("Error getting peer from kv", "peerID", packet.Repeater, "error", err)
			continue
		}
		// OpenBridge is always TS1
		packet.Slot = false
		_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(peer.IP),
			Port: peer.Port,
		})
		if err != nil {
			slog.Error("Error sending packet", "error", err)
		}
	}
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type", "packetType", packet.Signature)
		return
	}

	slog.Debug("Sending OpenBridge packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetPeer(ctx, repeaterIDBytes)
	if err != nil {
		slog.Error("Error getting repeater from kv", "repeaterID", repeaterIDBytes, "error", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       packet.Encode(),
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		slog.Error("Error marshalling packet", "error", err)
		return
	}
	if err := s.pubsub.Publish("openbridge:outgoing", packedBytes); err != nil {
		slog.Error("Error publishing packet to openbridge:outgoing", "error", err)
		return
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

func (s *Server) handlePacket(ctx context.Context, _ *net.UDPAddr, data []byte) {
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

	if !models.PeerIDExists(s.DB, peerID) {
		slog.Error("Unknown peer ID", "peerID", peerID)
		return
	}

	peer := models.FindPeerByID(s.DB, peerID)

	if !s.validateHMAC(ctx, packetBytes, hmacBytes, peer) {
		slog.Error("Invalid OpenBridge HMAC", "peerID", peerID)
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
		if rules.PeerShouldEgress(s.DB, p, &packet) {
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
