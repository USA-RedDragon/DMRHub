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

package hbrp

import (
	"context"
	"encoding/binary"
	"net"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/parrot"
	"github.com/USA-RedDragon/DMRHub/internal/dmrconst"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
	"k8s.io/klog/v2"
)

// Server is the DMR server.
type Server struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Started       bool
	Parrot        *parrot.Parrot
	DB            *gorm.DB
	Redis         redisClient
	CallTracker   *calltracker.CallTracker
}

const largestMessageSize = 302
const repeaterIDLength = 4
const bufferSize = 1000000 // 1MB

// MakeServer creates a new DMR server.
func MakeServer(db *gorm.DB, redis *redis.Client, callTracker *calltracker.CallTracker) Server {
	return Server{
		Buffer: make([]byte, largestMessageSize),
		SocketAddress: net.UDPAddr{
			IP:   net.ParseIP(config.GetConfig().ListenAddr),
			Port: config.GetConfig().DMRPort,
		},
		Started:     false,
		Parrot:      parrot.NewParrot(redis),
		DB:          db,
		Redis:       makeRedisClient(redis),
		CallTracker: callTracker,
	}
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) {
	// Send a MSTCL command to each repeater.
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Stop")
	defer span.End()

	repeaters, err := s.Redis.listRepeaters(ctx)
	if err != nil {
		klog.Errorf("Error scanning redis for repeaters", err)
	}
	for _, repeater := range repeaters {
		if config.GetConfig().Debug {
			klog.Infof("Repeater found: %d", repeater)
		}
		s.Redis.updateRepeaterConnection(ctx, repeater, "DISCONNECTED")
		repeaterBinary := make([]byte, repeaterIDLength)
		binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
		s.sendCommand(ctx, repeater, dmrconst.CommandMSTCL, repeaterBinary)
	}
	s.Started = false
}

func (s *Server) listen(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "hbrp:incoming")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Error unmarshalling packet", err)
			continue
		}
		s.handlePacket(ctx, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		}, packet.Data)
	}
}

func (s *Server) subscribePackets(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "hbrp:outgoing")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		var packet models.RawDMRPacket
		_, err := packet.UnmarshalMsg([]byte(msg.Payload))
		if err != nil {
			klog.Errorf("Error unmarshalling packet", err)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Data, &net.UDPAddr{
			IP:   net.ParseIP(packet.RemoteIP),
			Port: packet.RemotePort,
		})
		if err != nil {
			klog.Errorf("Error sending packet", err)
		}
	}
}

func (s *Server) subscribeRawPackets(ctx context.Context) {
	pubsub := s.Redis.Redis.Subscribe(ctx, "hbrp:outgoing:noaddr")
	defer func() {
		err := pubsub.Close()
		if err != nil {
			klog.Errorf("Error closing pubsub", err)
		}
	}()
	for msg := range pubsub.Channel() {
		packet := models.UnpackPacket([]byte(msg.Payload))
		repeater, err := s.Redis.getRepeater(ctx, packet.Repeater)
		if err != nil {
			klog.Errorf("Error getting repeater %d from redis", packet.Repeater)
			continue
		}
		_, err = s.Server.WriteToUDP(packet.Encode(), &net.UDPAddr{
			IP:   net.ParseIP(repeater.IP),
			Port: repeater.Port,
		})
		if err != nil {
			klog.Errorf("Error sending packet", err)
		}
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()
	server, err := net.ListenUDP("udp", &s.SocketAddress)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		klog.Exitf("Error opening UDP Socket", err)
	}

	s.Server = server
	s.Started = true

	klog.Infof("HBRP Server listening at %s on port %d", s.SocketAddress.IP.String(), s.SocketAddress.Port)

	go s.listen(ctx)
	go s.subscribePackets(ctx)
	go s.subscribeRawPackets(ctx)

	go func() {
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if config.GetConfig().Debug {
				klog.Infof("Read a message from %v\n", remoteaddr)
			}
			if err != nil {
				klog.Warningf("Error reading from UDP Socket, Swallowing Error: %v", err)
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
					klog.Errorf("Error marshalling packet", err)
					return
				}
				s.Redis.Redis.Publish(ctx, "hbrp:incoming", packedBytes)
			}()
		}
	}()
}

func (s *Server) sendCommand(ctx context.Context, repeaterIDBytes uint, command dmrconst.Command, data []byte) {
	if !s.Started && command != dmrconst.CommandMSTCL {
		klog.Warningf("Server not started, not sending command")
		return
	}
	if config.GetConfig().Debug {
		klog.Infof("Sending Command %s to Repeater ID: %d", command, repeaterIDBytes)
	}
	commandPrefixedData := append([]byte(command), data...)
	repeater, err := s.Redis.getRepeater(ctx, repeaterIDBytes)
	if err != nil {
		klog.Errorf("Error getting repeater from Redis", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       commandPrefixedData,
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling packet", err)
		return
	}
	s.Redis.Redis.Publish(ctx, "hbrp:outgoing", packedBytes)
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	if !s.Started {
		klog.Warningf("Server not started, not sending command")
		return
	}
	if config.GetConfig().Debug {
		klog.Infof("Sending Packet: %s\n", packet.String())
		klog.Infof("Sending DMR packet to Repeater ID: %d", repeaterIDBytes)
	}
	repeater, err := s.Redis.getRepeater(ctx, repeaterIDBytes)
	if err != nil {
		klog.Errorf("Error getting repeater from Redis", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       packet.Encode(),
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	packedBytes, err := p.MarshalMsg(nil)
	if err != nil {
		klog.Errorf("Error marshalling packet", err)
		return
	}
	s.Redis.Redis.Publish(ctx, "hbrp:outgoing", packedBytes)
}

func (s *Server) handlePacket(ctx context.Context, remoteAddr *net.UDPAddr, data []byte) {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.handlePacket")
	defer span.End()
	const signatureLength = 4
	if len(data) < signatureLength {
		// Not enough data here to be a valid packet
		klog.Warningf("Invalid packet length: %d", len(data))
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
		klog.Warning("TODO: RPTACK")
	case dmrconst.CommandMSTCL[:4]:
		klog.Warning("TODO: MSTCL")
	case dmrconst.CommandMSTNAK[:4]:
		klog.Warning("TODO: MSTNAK")
	case dmrconst.CommandMSTPONG[:4]:
		klog.Warning("TODO: MSTPONG")
	case dmrconst.CommandRPTSBKN[:4]:
		klog.Warning("TODO: RPTSBKN")
	default:
		klog.Warningf("Unknown command: %s", dmrconst.Command(data[:4]))
	}
}
