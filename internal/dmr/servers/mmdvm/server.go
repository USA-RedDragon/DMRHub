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

package mmdvm

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const channelBufferSize = 100

// Server is the DMR server.
type Server struct {
	Buffer        []byte
	config        *config.Config
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	DB            *gorm.DB
	kvClient      *servers.KVClient
	pubsub        pubsub.PubSub
	hub           *hub.Hub
	Version       string
	Commit        string
	incomingChan  chan models.RawDMRPacket
	outgoingChan  chan models.RawDMRPacket
	hubHandle     *hub.ServerHandle
	connected     *xsync.Map[uint, struct{}]
	stopped       atomic.Bool
	wg            sync.WaitGroup
	done          chan struct{}
	stopOnce      sync.Once
}

var (
	ErrOpenSocket   = errors.New("error opening socket")
	ErrSocketBuffer = errors.New("error setting socket buffer size")
)

const largestMessageSize = 302
const repeaterIDLength = 4
const bufferSize = 1000000 // 1MB

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, hub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, version, commit string) (Server, error) {
	socketAddr := net.UDPAddr{
		IP:   net.ParseIP(config.DMR.MMDVM.Bind),
		Port: config.DMR.MMDVM.Port,
	}
	server, err := net.ListenUDP("udp", &socketAddr)
	if err != nil {
		slog.Error("Error opening UDP Socket", "error", err)
		return Server{}, ErrOpenSocket
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		slog.Error("Error setting read buffer on UDP Socket", "error", err)
		return Server{}, ErrSocketBuffer
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		slog.Error("Error setting write buffer on UDP Socket", "error", err)
		return Server{}, ErrSocketBuffer
	}

	return Server{
		Buffer:        make([]byte, largestMessageSize),
		config:        config,
		SocketAddress: socketAddr,
		Server:        server,
		DB:            db,
		pubsub:        pubsub,
		kvClient:      servers.MakeKVClient(kv),
		hub:           hub,
		Version:       version,
		Commit:        commit,
		incomingChan:  make(chan models.RawDMRPacket, channelBufferSize),
		outgoingChan:  make(chan models.RawDMRPacket, channelBufferSize),
		connected:     xsync.NewMap[uint, struct{}](),
		done:          make(chan struct{}),
	}, nil
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Stop")
		defer span.End()

		s.stopped.Store(true)
		close(s.done)

		if s.hub != nil {
			s.hub.UnregisterServer(models.RepeaterTypeMMDVM)
		}

		// When other DMRHub instances are alive, skip sending MSTCL so
		// repeaters seamlessly migrate to the surviving instance instead of
		// disconnecting and entering a slow re-registration cycle.
		if !servers.IsGracefulHandoff(ctx) {
			var count int
			s.connected.Range(func(_ uint, _ struct{}) bool {
				count++
				return true
			})
			slog.Info("Stopping MMDVM server, sending MSTCL to connected repeaters", "count", count)

			s.connected.Range(func(repeater uint, _ struct{}) bool {
				slog.Info("Sending MSTCL to repeater", "repeater", repeater)
				repeaterInfo, err := s.kvClient.GetRepeater(ctx, repeater)
				if err != nil {
					slog.Error("Error getting repeater from KV", "repeater", repeater, "error", err)
					return true
				}
				repeaterBinary := make([]byte, repeaterIDLength)
				if repeater > 0xFFFFFFFF {
					slog.Error("Repeater ID too large for uint32", "repeater", repeater)
					return true
				}
				binary.BigEndian.PutUint32(repeaterBinary, uint32(repeater))
				mstclPayload := append([]byte(dmrconst.CommandMSTCL), repeaterBinary...)
				_, err = s.Server.WriteToUDP(mstclPayload, &net.UDPAddr{
					IP:   net.ParseIP(repeaterInfo.IP),
					Port: repeaterInfo.Port,
				})
				if err != nil {
					slog.Error("Error sending MSTCL command", "repeater", repeater, "error", err)
					return true
				}
				slog.Info("Sent MSTCL to repeater", "repeater", repeater, "ip", repeaterInfo.IP, "port", repeaterInfo.Port)
				repeaterInfo.Connection = "DISCONNECTED"
				s.kvClient.StoreRepeater(ctx, repeater, repeaterInfo)
				return true
			})
		} else {
			slog.Info("Graceful handoff: skipping MSTCL messages to connected repeaters")
		}

		if err := s.Server.Close(); err != nil {
			slog.Error("Error closing MMDVM UDP socket", "error", err)
		}
	})

	s.wg.Wait()
	return nil
}

func (s *Server) listen(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			slog.Info("Stopping MMDVM server listener")
			return
		case packet := <-s.incomingChan:
			s.handlePacket(ctx, net.UDPAddr{
				IP:   net.ParseIP(packet.RemoteIP),
				Port: packet.RemotePort,
			}, packet.Data)
		}
	}
}

func (s *Server) subscribePackets() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
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

// consumeHubPackets reads routed packets from the hub and sends them to repeaters.
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
			s.sendPacket(ctx, rp.RepeaterID, rp.Packet)
		}
	}
}

// Start starts the DMR server.
func (s *Server) Start(ctx context.Context) error {
	ctx, span := otel.Tracer("DMRHub").Start(ctx, "Server.Start")
	defer span.End()

	// Register with the hub to receive routed packets
	s.hubHandle = s.hub.RegisterServer(hub.ServerConfig{
		Name: models.RepeaterTypeMMDVM,
		Role: hub.RoleRepeater,
	})

	slog.Info("MMDVM Server listening", "address", s.SocketAddress.String())

	s.wg.Add(4)
	go s.listen(ctx)
	go s.subscribePackets()
	go s.consumeHubPackets(ctx)

	go func() {
		defer s.wg.Done()
		for {
			length, remoteaddr, err := s.Server.ReadFromUDP(s.Buffer)
			if err != nil {
				// When the socket is closed during shutdown, stop the loop quietly.
				if errors.Is(err, net.ErrClosed) {
					return
				}
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
	slog.Debug("Sending command", "command", command, "repeaterID", repeaterIDBytes)
	commandPrefixedData := append([]byte(command), data...)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		if errors.Is(err, servers.ErrNoSuchRepeater) {
			slog.Debug("Skipping command for repeater without active local session", "command", command, "repeaterID", repeaterIDBytes)
			return
		}
		slog.Error("Error getting repeater from KV", "repeaterID", repeaterIDBytes, "error", err)
		return
	}
	p := models.RawDMRPacket{
		Data:       commandPrefixedData,
		RemoteIP:   repeater.IP,
		RemotePort: repeater.Port,
	}
	s.outgoingChan <- p
}

func (s *Server) sendPacket(ctx context.Context, repeaterIDBytes uint, packet models.Packet) {
	// Only send to repeaters with active local sessions on this replica.
	// In multi-replica deployments, all replicas receive pubsub messages but
	// only the replica holding the UDP session should transmit.
	if _, ok := s.connected.Load(repeaterIDBytes); !ok {
		return
	}
	slog.Debug("Sending packet", "packet", packet.String(), "repeaterID", repeaterIDBytes)
	repeater, err := s.kvClient.GetRepeater(ctx, repeaterIDBytes)
	if err != nil {
		if errors.Is(err, servers.ErrNoSuchRepeater) {
			slog.Debug("Skipping packet for repeater without active local session", "repeaterID", repeaterIDBytes)
			return
		}
		slog.Error("Error getting repeater from KV", "repeaterID", repeaterIDBytes, "error", err)
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
	if s.stopped.Load() {
		return
	}

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
		s.handleDMRAPacket(ctx, data)
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
			s.handleRPTCLPacket(ctx, data)
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
