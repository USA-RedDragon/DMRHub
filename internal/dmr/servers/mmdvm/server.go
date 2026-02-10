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

package mmdvm

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
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const channelBufferSize = 100

// Server is the DMR server.
type Server struct {
	Buffer          []byte
	config          *config.Config
	SocketAddress   net.UDPAddr
	Server          *net.UDPConn
	Started         bool
	Parrot          *parrot.Parrot
	DB              *gorm.DB
	kvClient        *servers.KVClient
	pubsub          pubsub.PubSub
	CallTracker     *calltracker.CallTracker
	Version         string
	Commit          string
	incomingChan    chan models.RawDMRPacket
	outgoingChan    chan models.RawDMRPacket
	RawOutgoingChan chan []byte
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
			IP:   net.ParseIP(config.DMR.MMDVM.Bind),
			Port: config.DMR.MMDVM.Port,
		},
		Started:         false,
		Parrot:          parrot.NewParrot(kv),
		DB:              db,
		pubsub:          pubsub,
		kvClient:        servers.MakeKVClient(kv),
		CallTracker:     callTracker,
		Version:         version,
		Commit:          commit,
		incomingChan:    make(chan models.RawDMRPacket, channelBufferSize),
		outgoingChan:    make(chan models.RawDMRPacket, channelBufferSize),
		RawOutgoingChan: make(chan []byte, channelBufferSize),
	}
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) {
	// Send a MSTCL command to each repeater.
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Stop")
	defer span.End()

	repeaters, err := s.kvClient.ListRepeaters(ctx)
	if err != nil {
		slog.Error("Error scanning KV for repeaters", "error", err)
	}
	for _, repeater := range repeaters {
		slog.Debug("Repeater found", "repeater", repeater)
		s.kvClient.UpdateRepeaterConnection(ctx, repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, repeaterIDLength)
		if repeater > 0xFFFFFFFF {
			slog.Error("Repeater ID too large for uint32", "repeater", repeater)
			continue
		}
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(ctx, repeater, dmrconst.CommandMSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s *Server) listen(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			slog.Info("Stopping MMDVM server")
			return
		case packet := <-s.incomingChan:
			s.handlePacket(ctx, net.UDPAddr{
				IP:   net.ParseIP(packet.RemoteIP),
				Port: packet.RemotePort,
			}, packet.Data)
		}
	}
}

func (s *Server) subscribePackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case packet := <-s.outgoingChan:
			_, err := s.Server.WriteToUDP(packet.Data, &net.UDPAddr{
				IP:   net.ParseIP(packet.RemoteIP),
				Port: packet.RemotePort,
			})
			if err != nil {
				slog.Error("Error sending packet", "error", err)
			}
		}
	}
}

func (s *Server) subscribeRawPackets(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-s.RawOutgoingChan:
			packet, ok := models.UnpackPacket(msg)
			if !ok {
				slog.Error("Error unpacking packet")
				continue
			}
			repeater, err := s.kvClient.GetRepeater(ctx, packet.Repeater)
			if err != nil {
				slog.Error("Error getting repeater from KV", "repeaterID", packet.Repeater, "error", err)
				continue
			}
			_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
				IP:   net.ParseIP(repeater.IP),
				Port: repeater.Port,
			})
			if err != nil {
				slog.Error("Error sending packet", "error", err)
			}
		}
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		slog.Error("Error opening UDP Socket", "error", err)
		return ErrOpenSocket
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		slog.Error("Error setting read buffer on UDP Socket", "error", err)
		return ErrSocketBuffer
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		slog.Error("Error setting write buffer on UDP Socket", "error", err)
		return ErrSocketBuffer
	}

	s.Server = server
	s.Started = true

	// Wire up the raw outgoing channel to the subscription manager
	GetSubscriptionManager(s.DB).SetRawOutgoingChan(s.RawOutgoingChan)

	slog.Info("MMDVM Server listening", "address", s.SocketAddress.String())

	go s.listen(ctx)
	go s.subscribePackets(ctx)
	go s.subscribeRawPackets(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if err != nil {
				slog.Error("Error reading from UDP Socket, Swallowing Error", "error", err)
				continue
			}
			slog.Debug("Read message from UDP socket", "remoteaddr", remoteaddr, "length", length)
			// Copy the buffer data since s.Buffer will be reused for the next read
			data := make([]byte, length)
			copy(data, s.Buffer[:length])
			p := models.RawDMRPacket{
				Data:       data,
				RemoteIP:   remoteaddr.IP.String(),
				RemotePort: remoteaddr.Port,
			}
			s.incomingChan <- p
		}
	}()

	return nil
}

func (s *Server) sendCommand(ctx context.Context, repeaterIDBytes uint, command dmrconst.Command, data []byte) {
	if !s.Started && command != dmrconst.CommandMSTCL {
		slog.Error("Server not started, not sending command")
		return
	}
	slog.Debug("Sending command", "command", command, "repeaterID", repeaterIDBytes)
	commandPrefixedData := append([]byte(command), data...)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		slog.Error("Error getting repeater from KV", "error", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       commandPrefixedData,
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	s.outgoingChan <- p
}

func (s *Server) sendOpenBridgePacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type", "signature", packet.Signature)
		return
	}

	slog.Debug("Sending OpenBridge packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetPeer(ctx, repeaterIDBytes)
	if err != nil {
		slog.Error("Error getting repeater from KV", "error", err)
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

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if !s.Started {
		slog.Error("Server not started, not sending command")
		return
	}
	slog.Debug("Sending packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		slog.Error("Error getting repeater from KV", "error", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       packet.Encode(),
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	s.outgoingChan <- p
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()
	const signatureLength = 4
	if len(data) < signatureLength {
		// Not enough data here to be a valid packet
		slog.Error("Invalid packet length", "length", len(data))
		return
	}

	switch dmrconst.Command(data[:4]) { //nolint:exhaustive
	case dmrconst.CommandDMRA:
		s.handleDMRAPacket(ctx, remoteAddr, data)
	case dmrconst.CommandDMRD:
		s.handleDMRDPacket(ctx, remoteAddr, data)
	case dmrconst.CommandRPTO:
		// https://github.com/g4klx/MMDVMHost/blob/master/DMRplus_startup_options.md
		// Options are not yet supported
		slog.Error("TODO: RPTO")
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
		slog.Error("TODO: RPTACK")
	case dmrconst.CommandMSTCL[:4]:
		slog.Error("TODO: MSTCL")
	case dmrconst.CommandMSTNAK[:4]:
		slog.Error("TODO: MSTNAK")
	case dmrconst.CommandMSTPONG[:4]:
		slog.Error("TODO: MSTPONG")
	case dmrconst.CommandRPTSBKN[:4]:
		slog.Error("TODO: RPTSBKN")
	default:
		slog.Error("Unknown command", "command", dmrconst.Command(data[:4]))
	}
}
