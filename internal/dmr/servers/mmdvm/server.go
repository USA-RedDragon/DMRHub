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
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/wtcert"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
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
	// stamped reports that the kernel stamps the packets of Server with
	// the time they came.
	stamped      bool
	DB           *gorm.DB
	kvClient     *servers.KVClient
	kv           kv.KV
	pubsub       pubsub.PubSub
	hub          *hub.Hub
	Version      string
	Commit       string
	incomingChan chan models.RawDMRPacket
	outgoingChan chan outgoing
	hubHandle    *hub.ServerHandle
	stopClaims   func()
	stopSecrets  func()
	sentMu       sync.Mutex
	sent         map[uint][]*sentPacket
	movingMu     sync.Mutex
	moving       map[uint]*movingRepeater
	wt           *webTransport
	connected    *xsync.Map[uint, endpoint]
	transports   *xsync.Map[string, transport]
	egressMiss   *xsync.Map[uint, time.Time]
	lookups      sync.WaitGroup
	// records runs the KV, database and hub work of each repeater off the
	// listener.
	records servers.RepeaterQueue
	// egressWaits holds the packets for each repeater that is being
	// resumed for egress.
	egressMu    sync.Mutex
	egressWaits map[uint][]models.Packet
	egressWG    sync.WaitGroup
	stopped     atomic.Bool
	wg          sync.WaitGroup
	done        chan struct{}
	stopOnce    sync.Once

	mu    sync.Mutex
	links map[string]*link
	byID  map[uint32]*link
	// rekeys counts the changes of the password of each repeater, so that
	// a resume that read the record before one never takes the session.
	rekeys map[uint32]uint64
	// resolving holds the packets from each address that wait for the KV
	// record of their repeater.
	resolving map[string][]heldPacket
	// known holds the IDs of the repeaters, once loaded; see login.
	known    map[uint32]bool
	misses   int
	missesAt time.Duration
	epoch    time.Time
	// newSalt, when set, makes the login salts in place of crypto/rand.
	newSalt func() (uint32, error)
	// resumeKey signs the tokens that let a browser hotspot move to another
	// replica.
	resumeKey []byte

	ownMu      sync.Mutex
	ownRecords map[uint]models.Repeater

	pingMu      sync.Mutex
	pings       map[uint]time.Time
	pinging     bool
	pingsClosed bool
	pingWG      sync.WaitGroup
}

var (
	ErrOpenSocket   = errors.New("error opening socket")
	ErrSocketBuffer = errors.New("error setting socket buffer size")
)

const bufferSize = 4 << 20

// readBufferSize is one octet longer than the longest packet that the
// sessions take, so that a read of a longer packet, which the socket or the
// transport cuts to the buffer, shows that it was longer.
const readBufferSize = dmrdsession.MaxPacket + 1

// MakeServer creates a new DMR server.
func MakeServer(config *config.Config, hub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, kv kv.KV, version, commit string) (Server, error) {
	server, socketAddr, err := servers.ListenUDP(config.DMR.MMDVM.Bind, config.DMR.MMDVM.Port)
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

	stamped := servers.StampArrivals(server)

	var wt *webTransport
	if c := config.DMR.MMDVM.WebTransport; c.Enabled {
		certs, err := wtcert.Shared(config.GetDerivedSecret(), c.CertFile, c.KeyFile)
		if err != nil {
			_ = server.Close()
			return Server{}, fmt.Errorf("WebTransport certificate: %w", err)
		}
		if wt, err = listenWebTransport(config.DMR.MMDVM.Bind, c.Port, certs, config.GetDerivedSecret(), c.AllowedOrigins, pubsub, kv); err != nil {
			_ = server.Close()
			return Server{}, err
		}
	}

	return Server{
		wt:            wt,
		Buffer:        make([]byte, readBufferSize),
		config:        config,
		SocketAddress: socketAddr,
		Server:        server,
		stamped:       stamped,
		DB:            db,
		pubsub:        pubsub,
		kvClient:      servers.MakeKVClient(kv),
		kv:            kv,
		hub:           hub,
		Version:       version,
		Commit:        commit,
		incomingChan:  make(chan models.RawDMRPacket, channelBufferSize),
		outgoingChan:  make(chan outgoing, channelBufferSize),
		connected:     xsync.NewMap[uint, endpoint](),
		transports:    xsync.NewMap[string, transport](),
		egressMiss:    xsync.NewMap[uint, time.Time](),
		done:          make(chan struct{}),
		resumeKey:     resumeKey(config.GetDerivedSecret()),
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
		if s.stopClaims != nil {
			s.stopClaims()
		}
		if s.stopSecrets != nil {
			s.stopSecrets()
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
			s.wt.unregister(ctx)
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
	s.egressWG.Wait()
	s.records.Wait()
	s.closePings()
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
			s.handleFrom(ctx, endpointOf(packet), packet.Data, packet.ReceivedAt)
		}
	}
}

func (s *Server) subscribePackets() {
	defer s.wg.Done()
	for {
		select {
		case <-s.done:
			return
		case o := <-s.outgoingChan:
			s.writeOut(o)
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
			if rp.Barrier != nil {
				close(rp.Barrier)
				continue
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
	s.stopClaims = s.hub.OnClaimedElsewhere(s.dropMoved)
	s.stopSecrets = s.hub.OnRepeaterSecretChange(func(id uint) { s.endSessions(ctx, id) })

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
		resume := s.resumeWebTransport
		s.wt.onResume.Store(&resume)
		moved := s.resendMoved
		s.wt.onPathMoved.Store(&moved)
		slog.Info("MMDVM WebTransport listening", "address", s.wt.Addr().String(), "path", wtcert.Path)
		s.transports.Store(webTransportName, s.wt)
		s.wg.Add(1)
		go s.serve(webTransportName, s.wt, make([]byte, readBufferSize))
	}

	return nil
}

// serve reads the packets of the transport t, named name, until it is
// closed, and queues them for the sessions.
func (s *Server) serve(name string, t transport, buf []byte) {
	defer s.wg.Done()
	read := s.readerFor(t)
	for {
		length, remote, at, err := read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			slog.Error("Error reading from MMDVM transport, swallowing error", "transport", name, "error", err)
			continue
		}
		slog.Debug("Read message from MMDVM transport", "transport", name, "remoteaddr", remote, "length", length)
		if length > dmrdsession.MaxPacket {
			slog.Debug("Dropping a packet longer than the sessions take", "transport", name, "remoteaddr", remote, "length", length)
			metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonOversize)
			continue
		}
		if s.discard(buf[:length]) {
			continue
		}
		data := make([]byte, length)
		copy(data, buf[:length])
		packet := rawPacketFrom(endpoint{transport: name, addr: remote}, data)
		packet.ReceivedAt = at
		select {
		case s.incomingChan <- packet:
		case <-s.done:
			return
		}
	}
}

// readerFor returns a func that reads a packet of t and the time it came,
// as servers.ReadStamped tells it for the UDP socket.
func (s *Server) readerFor(t transport) func([]byte) (int, net.Addr, time.Time, error) {
	c, ok := t.(*net.UDPConn)
	if !ok || c != s.Server {
		return func(b []byte) (int, net.Addr, time.Time, error) {
			n, addr, err := t.ReadFrom(b)
			return n, addr, time.Now(), err //nolint:wrapcheck // the caller looks for net.ErrClosed
		}
	}
	oob := make([]byte, servers.RxOOBLen)
	return func(b []byte) (int, net.Addr, time.Time, error) {
		n, addr, at, err := servers.ReadStamped(c, s.stamped, b, oob)
		if err != nil {
			return n, nil, at, err //nolint:wrapcheck // the caller looks for net.ErrClosed
		}
		return n, addr, at, nil
	}
}

// discard reports whether the reader drops p before it is queued: a
// packet with no tag of either dialect, which the sessions would ignore,
// or a DMRD packet of a repeater slot that has sent more than the hub
// takes (see hub.Hub.SlotOverRate). A flood of either is dropped as fast
// as it comes, so that it never fills the socket that other repeaters
// share.
func (s *Server) discard(p []byte) bool {
	c := dmrd.Classify(p, dmrd.DialectMMDVM)
	if c == dmrd.CommandUnknown && dmrd.Classify(p, dmrd.DialectHBRP) == dmrd.CommandUnknown {
		return true
	}
	if s.hub == nil || c != dmrd.CommandDMRD {
		return false
	}
	d, err := dmrd.DecodeData(p)
	if err != nil || !s.hub.SlotOverRate(models.RepeaterTypeMMDVM, uint(d.Repeater), d.Slot == enums.Slot2) {
		return false
	}
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonSlotRate)
	return true
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
	if s.hold(repeaterID, packet) {
		return
	}
	s.deliver(ctx, repeaterID, packet)
}

// deliver sends packet to the repeater repeaterID when this replica holds
// its session.
func (s *Server) deliver(ctx context.Context, repeaterID uint, packet models.Packet) {
	// Only send to repeaters with active local sessions on this replica.
	// In multi-replica deployments, all replicas receive pubsub messages but
	// only the replica holding the UDP session should transmit.
	s.egressMu.Lock()
	if held, ok := s.egressWaits[repeaterID]; ok {
		if len(held) < maxHeldPerAddress {
			s.egressWaits[repeaterID] = append(held, packet)
		}
		s.egressMu.Unlock()
		return
	}
	ep, ok := s.connected.Load(repeaterID)
	if !ok {
		if !s.mayResumeForEgress(repeaterID) {
			s.egressMu.Unlock()
			return
		}
		if s.egressWaits == nil {
			s.egressWaits = map[uint][]models.Packet{}
		}
		s.egressWaits[repeaterID] = []models.Packet{packet}
		s.egressMu.Unlock()
		s.egressWG.Add(1)
		go s.resumeAndSend(ctx, repeaterID)
		return
	}
	s.egressMu.Unlock()
	s.send(ep, repeaterID, packet)
}

func (s *Server) send(ep endpoint, repeaterID uint, packet models.Packet) {
	slog.Debug("Sending packet", "packet", packet.String(), "repeaterID", repeaterID)
	sent := s.remember(repeaterID, packet)
	select {
	case s.outgoingChan <- outgoing{raw: rawPacketFrom(ep, packet.Encode()), sent: sent}:
	case <-s.done:
		s.forgetPacket(repeaterID, sent)
	}
}

// resumeAndSend resumes the session of the repeater id for egress off the
// hub consumer, then sends the packets that waited for it, in order.
func (s *Server) resumeAndSend(ctx context.Context, id uint) {
	defer s.egressWG.Done()
	ep, ok := s.resumeForEgress(ctx, id)
	for {
		s.egressMu.Lock()
		held := s.egressWaits[id]
		if len(held) == 0 {
			delete(s.egressWaits, id)
			s.egressMu.Unlock()
			return
		}
		packet := held[0]
		s.egressWaits[id] = held[1:]
		s.egressMu.Unlock()
		if ok {
			s.send(ep, id, packet)
		}
	}
}

// outgoing is a packet that waits for the writer, and sent is the record
// of it for a handover.
type outgoing struct {
	raw  models.RawDMRPacket
	sent *sentPacket
}

// writeOut writes o, and then records when it left. Until then the
// packet counts as one that waits, which a handover holds, so that a write
// that stalls past a move is not missed.
func (s *Server) writeOut(o outgoing) {
	s.write(endpointOf(o.raw), o.raw.Data)
	if o.sent != nil {
		s.sentMu.Lock()
		o.sent.at = time.Now()
		s.sentMu.Unlock()
	}
}
