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
	"errors"
	"fmt"
	"log/slog"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
	"github.com/puzpuzpuz/xsync/v4"
	"go.opentelemetry.io/otel"
	"gorm.io/gorm"
)

const channelBufferSize = 100

// tickInterval is how often the server runs the timers of the sessions.
const tickInterval = time.Second

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
	wt            *webTransport
	connected     *xsync.Map[uint, endpoint]
	transports    *xsync.Map[string, transport]
	egressMiss    *xsync.Map[uint, time.Time]
	lookups       sync.WaitGroup
	stopped       atomic.Bool
	wg            sync.WaitGroup
	done          chan struct{}
	stopOnce      sync.Once

	mu    sync.Mutex
	links map[string]*link
	byID  map[uint32]*link
	// known holds the IDs of the repeaters, once loaded; see login.
	known    map[uint32]bool
	misses   int
	missesAt time.Duration
	epoch    time.Time
	// newSalt, when set, makes the login salts in place of crypto/rand.
	newSalt func() (uint32, error)
}

var (
	ErrOpenSocket   = errors.New("error opening socket")
	ErrSocketBuffer = errors.New("error setting socket buffer size")
)

const bufferSize = 1000000 // 1MB

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, hub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, version, commit string) (Server, error) {
	socketAddr := net.UDPAddr{
		IP:   net.ParseIP(config.DMR.MMDVM.Bind),
		Port: config.DMR.MMDVM.Port,
	}
	server, err := net.ListenUDP("udp", &socketAddr)
	if err != nil {
		return Server{}, fmt.Errorf("%w: %w", ErrOpenSocket, err)
	}

	if err := server.SetReadBuffer(bufferSize); err != nil {
		_ = server.Close()
		return Server{}, fmt.Errorf("%w: %w", ErrSocketBuffer, err)
	}
	if err := server.SetWriteBuffer(bufferSize); err != nil {
		_ = server.Close()
		return Server{}, fmt.Errorf("%w: %w", ErrSocketBuffer, err)
	}

	var wt *webTransport
	if c := config.DMR.MMDVM.WebTransport; c.Enabled {
		certs, err := wtcert.New(config.GetDerivedSecret(), c.CertFile, c.KeyFile)
		if err != nil {
			_ = server.Close()
			return Server{}, fmt.Errorf("WebTransport certificate: %w", err)
		}
		if wt, err = listenWebTransport(config.DMR.MMDVM.Bind, c.Port, certs); err != nil {
			_ = server.Close()
			return Server{}, err
		}
	}

	return Server{
		wt:            wt,
		Buffer:        make([]byte, dmrdsession.MaxPacket),
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
		connected:     xsync.NewMap[uint, endpoint](),
		transports:    xsync.NewMap[string, transport](),
		egressMiss:    xsync.NewMap[uint, time.Time](),
		done:          make(chan struct{}),
	}, nil
}

// WebTransportAddr returns the address of the UDP socket of the
// WebTransport listener, or nil when it is off.
func (s *Server) WebTransportAddr() net.Addr {
	if s.wt == nil {
		return nil
	}
	return s.wt.Addr()
}

// Addr returns the address of the socket of the server.
func (s *Server) Addr() net.Addr {
	return s.Server.LocalAddr()
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
			s.closeSessions(ctx)
		} else {
			slog.Info("Graceful handoff: skipping MSTCL messages to connected repeaters")
		}

		if err := s.Server.Close(); err != nil {
			slog.Error("Error closing MMDVM UDP socket", "error", err)
		}
		if s.wt != nil {
			if err := s.wt.Close(); err != nil {
				slog.Error("Error closing MMDVM WebTransport listener", "error", err)
			}
		}
		s.transports.Range(func(name string, t transport) bool {
			if name != udpTransport {
				if err := t.Close(); err != nil {
					slog.Error("Error closing MMDVM transport", "transport", name, "error", err)
				}
			}
			return true
		})
	})

	s.wg.Wait()
	s.lookups.Wait()
	return nil
}

func (s *Server) listen(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			slog.Info("Stopping MMDVM server listener")
			return
		case <-ticker.C:
			s.tick(ctx)
		case packet := <-s.incomingChan:
			s.handleFrom(ctx, endpointOf(packet), packet.Data)
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
			s.write(endpointOf(packet), packet.Data)
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

	if s.DB != nil {
		if err := s.loadKnown(); err != nil {
			slog.Error("Error loading the repeater IDs, every login is looked up", "error", err)
		}
		s.wg.Add(1)
		go s.refreshKnown()
	}

	s.wg.Add(4)
	go s.listen(ctx)
	go s.subscribePackets()
	go s.consumeHubPackets(ctx)

	s.transports.Store(udpTransport, s.Server)
	go s.serve(udpTransport, s.Server, s.Buffer)

	if s.wt != nil {
		slog.Info("MMDVM WebTransport listening", "address", s.wt.Addr().String(), "path", wtcert.Path)
		s.transports.Store(webTransportName, s.wt)
		s.wg.Add(1)
		go s.serve(webTransportName, s.wt, make([]byte, dmrdsession.MaxPacket))
	}

	return nil
}

// serve reads the packets of the transport t, named name, until it is
// closed, and queues them for the sessions.
func (s *Server) serve(name string, t transport, buf []byte) {
	defer s.wg.Done()
	for {
		length, remote, err := t.ReadFrom(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("Error reading from MMDVM transport, swallowing error", "transport", name, "error", err)
			continue
		}
		slog.Debug("Read message from MMDVM transport", "transport", name, "remoteaddr", remote, "length", length)
		data := make([]byte, length)
		copy(data, buf[:length])
		select {
		case s.incomingChan <- rawPacketFrom(endpoint{transport: name, addr: remote}, data):
		case <-s.done:
			return
		}
	}
}

func (s *Server) transport(name string) (transport, bool) {
	if name == udpTransport && s.Server != nil {
		return s.Server, true
	}
	if s.transports == nil {
		return nil, false
	}
	return s.transports.Load(name)
}

func rawPacketFrom(ep endpoint, data []byte) models.RawDMRPacket {
	p := models.RawDMRPacket{Data: data, Transport: ep.transport, Addr: ep.addr}
	if a, ok := ep.addr.(*net.UDPAddr); ok && ep.transport == udpTransport {
		p.RemoteIP, p.RemotePort = a.IP.String(), a.Port
	}
	return p
}

func endpointOf(p models.RawDMRPacket) endpoint {
	if p.Addr != nil {
		return endpoint{transport: p.Transport, addr: p.Addr}
	}
	return udpEndpoint(net.UDPAddr{IP: net.ParseIP(p.RemoteIP), Port: p.RemotePort})
}

func (s *Server) sendPacket(ctx context.Context, repeaterID uint, packet models.Packet) {
	// Only send to repeaters with active local sessions on this replica.
	// In multi-replica deployments, all replicas receive pubsub messages but
	// only the replica holding the UDP session should transmit.
	ep, ok := s.connected.Load(repeaterID)
	if !ok {
		ep, ok = s.resumeForEgress(ctx, repeaterID)
		if !ok {
			return
		}
	}
	slog.Debug("Sending packet", "packet", packet.String(), "repeaterID", repeaterID)
	select {
	case s.outgoingChan <- rawPacketFrom(ep, packet.Encode()):
	case <-s.done:
	}
}
