// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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

package hbrp

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/parrot"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/logging"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

// Server is the DMR server.
type Server struct {
	Buffer        []byte
	config        *config.Config
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Started       bool
	Parrot        *parrot.Parrot
	DB            *gorm.DB
	kvClient      *servers.KVClient
	pubsub        pubsub.PubSub
	CallTracker   *calltracker.CallTracker
	Version       string
	Commit        string
}

var (
	ErrOpenSocket   = errors.New("error opening socket")
	ErrSocketBuffer = errors.New("error setting socket buffer size")
)

const largestMessageSize = 302
const repeaterIDLength = 4
const bufferSize = 1000000 // 1MB

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, callTracker *calltracker.CallTracker, version, commit string) Server {
	return Server{
		Buffer: make([]byte, largestMessageSize),
		config: config,
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(config.DMR.HBRP.Bind),
			Port: config.DMR.HBRP.Port,
		},
		Started:     false,
		Parrot:      parrot.NewParrot(kv),
		DB:          db,
		pubsub:      pubsub,
		kvClient:    servers.MakeKVClient(kv),
		CallTracker: callTracker,
		Version:     version,
		Commit:      commit,
	}
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) {
	// Send a MSTCL command to each repeater.
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Stop")
	defer span.End()

	repeaters, err := s.kvClient.ListRepeaters(ctx)
	if err != nil {
		logging.Errorf("Error scanning KV for repeaters: %v", err)
	}
	for _, repeater := range repeaters {
		slog.Debug("Repeater found", "repeater", repeater)
		s.kvClient.UpdateRepeaterConnection(ctx, repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, repeaterIDLength)
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(ctx, repeater, dmrconst.CommandMSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s *Server) listen(ctx context.Context) {
	pubsub := s.pubsub.Subscribe("hbrp:incoming")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub: %v", err)
		}
	}()
	pubsubChannel := pubsub.Channel()
	for {
		select {
		case <-ctx.Done():
			logging.Log("Stopping HBRP server")
			return
		case msg := <-pubsubChannel:
			var packet models.RawDMRPacket
			_, err := packet.UnmarshalMsg(msg)
			if err != nil {
				logging.Errorf("Error unmarshalling packet: %v", err)
				continue
			}
			s.handlePacket(ctx, net.UDPAddr{
				IP:   net.ParseIP(packet.RemoteIP),
				Port: packet.RemotePort,
			}, packet.Data)
		}
	}
}

func (s *Server) subscribePackets() {
	pubsub := s.pubsub.Subscribe("hbrp:outgoing")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub: %v", err)
		}
	}()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg(msg)
		if err != nil {
			logging.Errorf("Error unmarshalling packet: %v", err)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Data, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		})
		if err != nil {
			logging.Errorf("Error sending packet: %v", err)
		}
	}
}

func (s *Server) subscribeRawPackets(ctx context.Context) {
	pubsub := s.pubsub.Subscribe("hbrp:outgoing:noaddr")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			logging.Errorf("Error closing pubsub: %v", err)
		}
	}()
	for msg := range pubsub.Channel() {
		packet, ok := models.UnpackPacket(msg)
		if !ok {
			logging.Error("Error unpacking packet")
			continue
		}
		repeater, err := s.kvClient.GetRepeater(ctx, packet.Repeater)
		if err != nil {
			logging.Errorf("Error getting repeater %d from KV: %v", packet.Repeater, err)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(repeater.IP),
			Port: repeater.Port,
		})
		if err != nil {
			logging.Errorf("Error sending packet: %v", err)
		}
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		logging.Errorf("Error opening UDP Socket: %v", err)
		return ErrOpenSocket
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		logging.Errorf("Error setting read buffer on UDP Socket: %v", err)
		return ErrSocketBuffer
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		logging.Errorf("Error setting write buffer on UDP Socket: %v", err)
		return ErrSocketBuffer
	}

	s.Server = server
	s.Started = true

	logging.Errorf("HBRP Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)

	go s.listen(ctx)
	go s.subscribePackets()
	go s.subscribeRawPackets(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if err != nil {
				logging.Errorf("Error reading from UDP Socket, Swallowing Error: %v", err)
				continue
			}
			slog.Debug("Read message from UDP socket", "remoteaddr", remoteaddr, "length", length)
			p := models.RawDMRPacket{
				Data:       s.Buffer[:length],
				RemoteIP:   remoteaddr.IP.String(),
				RemotePort: remoteaddr.Port,
			}
			packedBytes, err := p.MarshalMsg(nil)
			if err != nil {
				logging.Errorf("Error marshalling packet: %v", err)
				return
			}
			if err := s.pubsub.Publish("hbrp:incoming", packedBytes); err != nil {
				logging.Errorf("Error publishing packet to hbrp:incoming: %v", err)
				return
			}
		}
	}()

	return nil
}

func (s *Server) sendCommand(ctx context.Context, repeaterIDBytes uint, command dmrconst.Command, data []byte) {
	if !s.Started && command != dmrconst.CommandMSTCL {
		logging.Errorf("Server not started, not sending command")
		return
	}
	slog.Debug("Sending command", "command", command, "repeaterID", repeaterIDBytes)
	commandPrefixedData := append([]byte(command), data...)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       commandPrefixedData,
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		logging.Errorf("Error marshalling packet: %v", err)
		return
	}
	if err := s.pubsub.Publish("hbrp:outgoing", packedBytes); err != nil {
		logging.Errorf("Error publishing packet to hbrp:outgoing: %v", err)
		return
	}
}

func (s *Server) sendOpenBridgePacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		logging.Errorf("Invalid packet type: %s", packet.Signature)
		return
	}

	slog.Debug("Sending OpenBridge packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetPeer(ctx, repeaterIDBytes)
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
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
	if err := s.pubsub.Publish("openbridge:outgoing", packedBytes); err != nil {
		logging.Errorf("Error publishing packet to openbridge:outgoing: %v", err)
		return
	}
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if !s.Started {
		logging.Errorf("Server not started, not sending command")
		return
	}
	slog.Debug("Sending packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		logging.Errorf("Error getting repeater from KV: %v", err)
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
	if err := s.pubsub.Publish("hbrp:outgoing", packedBytes); err != nil {
		logging.Errorf("Error publishing packet to hbrp:outgoing: %v", err)
		return
	}
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()
	const signatureLength = 4
	if len(data) < signatureLength {
		// Not enough data here to be a valid packet
		logging.Errorf("Invalid packet length: %d", len(data))
		return
	}

	switch dmrconst.Command(data[:4]) { //nolint:golint,exhaustive
	case dmrconst.CommandDMRA:
		s.handleDMRAPacket(ctx, remoteAddr, data)
	case dmrconst.CommandDMRD:
		s.handleDMRDPacket(ctx, remoteAddr, data)
	case dmrconst.CommandRPTO:
		s.handleRPTOPacket(ctx, remoteAddr, data)
	case dmrconst.CommandRPTL:
		s.handleRPTLPacket(ctx, remoteAddr, data)
	case dmrconst.CommandRPTK:
		s.handleRPTKPacket(ctx, remoteAddr, data)
	case dmrconst.CommandRPTC:
		if dmrconst.Command(data[:5]) == dmrconst.CommandRPTCL {
			s.handleRPTCLPacket(ctx, remoteAddr, data)
		} else {
			s.handleRPTCPacket(ctx, remoteAddr, data)
		}
	case dmrconst.CommandRPTPING[:4]:
		s.handleRPTPINGPacket(ctx, remoteAddr, data)
	// I don't think we ever receive these
	case dmrconst.CommandRPTACK[:4]:
		logging.Error("TODO: RPTACK")
	case dmrconst.CommandMSTCL[:4]:
		logging.Error("TODO: MSTCL")
	case dmrconst.CommandMSTNAK[:4]:
		logging.Error("TODO: MSTNAK")
	case dmrconst.CommandMSTPONG[:4]:
		logging.Error("TODO: MSTPONG")
	case dmrconst.CommandRPTSBKN[:4]:
		logging.Error("TODO: RPTSBKN")
	default:
		logging.Errorf("Unknown command: %s", dmrconst.Command(data[:4]))
	}
}
