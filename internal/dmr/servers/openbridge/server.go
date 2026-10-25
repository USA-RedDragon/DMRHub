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
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
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
	keepaliveTick    = time.Second
)

// maxHops is the most OpenBridge links a call may cross, as FreeDMR's
// MAX_FBP_HOPS in hblink.py.
const maxHops = 20

// enhancedVersion is the DMRE version that DMRHub sends to enhanced peers,
// the newest of FreeDMR (hblink.py send_system).
const enhancedVersion = 5

// origin is where a call entered OpenBridge: the source server and
// repeater, and the number of links it crossed. The zero origin is a call
// from a local repeater or a version 1 peer.
type origin struct {
	server   uint32
	repeater uint32
	hops     uint8
}

// OpenBridge is the same as HBRP, but with a single packet type.
type Server struct {
	Buffer        []byte
	SocketAddress net.UDPAddr
	Server        *net.UDPConn
	Tracer        trace.Tracer

	DB       *gorm.DB
	pubsub   pubsub.PubSub
	hub      *hub.Hub
	serverID uint32

	hubHandle *hub.ServerHandle
	ingress   *streamorder.Queue
	wg        sync.WaitGroup
	done      chan struct{}
	stopOnce  sync.Once

	bridgesMu sync.Mutex
	bridges   map[uint]*bridge
	epoch     time.Time

	quenched      *xsync.Map[quench, time.Time]
	quenchChecked *xsync.Map[quench, time.Time]

	kv     kv.KV
	id     string
	leader atomic.Bool
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
	enhanced bool
	b        *dmrdsession.Bridge
	key      *dmrdauth.BridgeKey
}

func (s *Server) bridgeFor(peer models.Peer) (*bridge, error) {
	s.bridgesMu.Lock()
	defer s.bridgesMu.Unlock()
	if b, ok := s.bridges[peer.ID]; ok && b.password == peer.Password && b.enhanced == peer.Enhanced {
		return b, nil
	}
	sb, err := dmrdsession.NewBridge(&dmrdsession.BridgeConfig{Key: []byte(peer.Password), Enhanced: peer.Enhanced})
	if err != nil {
		return nil, fmt.Errorf("failed to make OpenBridge link: %w", err)
	}
	if s.bridges == nil {
		s.bridges = map[uint]*bridge{}
	}
	b := &bridge{password: peer.Password, enhanced: peer.Enhanced, b: sb, key: dmrdauth.NewBridgeKey([]byte(peer.Password))}
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
// signature. For an enhanced link it is also a sign of life of the peer.
func (b *bridge) verifiedControl(now time.Duration, data []byte) bool {
	if _, ok := b.key.Verify(data); !ok {
		return false
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	b.b.Packet(now, data, buf[:0])
	return true
}

// tick runs the keepalive of an enhanced link and returns the packets to
// send to the peer.
func (b *bridge) tick(now time.Duration) [][]byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	var buf [dmrdsession.MaxOutputs]dmrdsession.Output
	var out [][]byte
	for _, o := range b.b.Tick(now, buf[:0]) {
		switch o.Kind { //nolint:exhaustive
		case dmrdsession.OutputSend:
			out = append(out, o.Packet)
		case dmrdsession.OutputDown:
			slog.Info("Enhanced OpenBridge peer sent nothing for a minute")
		}
	}
	return out
}

func (b *bridge) sign(p []byte) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if err := b.b.SignInPlace(p); err != nil {
		return fmt.Errorf("failed to sign OpenBridge packet: %w", err)
	}
	return nil
}

// MakeServer creates a new DMR server. kvStore holds the egress lease and
// the source quenches that replicas share; with a nil kvStore the server
// works alone.
func MakeServer(config *config.Config, hub *hub.Hub, db *gorm.DB, pubsub pubsub.PubSub, kvStore kv.KV) (Server, error) {
	server, socketAddress, err := servers.ListenUDP(config.DMR.OpenBridge.Bind, config.DMR.OpenBridge.Port)
	if err != nil {
		return Server{}, fmt.Errorf("error opening OpenBridge socket: %w", err)
	}

	err = server.SetReadBuffer(bufferSize)
	if err != nil {
		_ = server.Close()
		return Server{}, fmt.Errorf("error setting UDP Socket read buffer: %w", err)
	}
	err = server.SetWriteBuffer(bufferSize)
	if err != nil {
		_ = server.Close()
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
		serverID:      config.DMR.OpenBridge.ServerID,
		done:          make(chan struct{}),
		epoch:         time.Now(),
		quenched:      xsync.NewMap[quench, time.Time](),
		quenchChecked: xsync.NewMap[quench, time.Time](),
		kv:            kvStore,
		id:            newReplicaID(),
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

	s.leader.Store(s.holdLease(ctx))
	s.wg.Add(5)
	go s.consumeHubPackets(ctx)
	go s.keepalive()
	go s.runLease(context.WithoutCancel(ctx))
	go s.consumeEgress(ctx)

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

// Addr returns the address of the socket of the server.
func (s *Server) Addr() net.Addr {
	return s.Server.LocalAddr()
}

// Stop stops the DMR server.
func (s *Server) Stop(ctx context.Context) error {
	s.stopOnce.Do(func() {
		slog.Info("Stopping OpenBridge server")

		close(s.done)
		s.releaseLease(ctx)
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
			s.sendPacketToPeer(ctx, rp.RepeaterID, rp.Packet, origin{repeater: uint32(rp.Packet.Repeater)}) //nolint:gosec
		}
	}
}

// keepalive sends the BCKA of each enhanced peer when it is due.
func (s *Server) keepalive() {
	defer s.wg.Done()
	ticker := time.NewTicker(keepaliveTick)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case <-ticker.C:
		}
		if !s.leader.Load() {
			continue
		}
		peers, err := models.ListPeers(s.DB)
		if err != nil {
			slog.Error("Failed to list peers for OpenBridge keepalives", "error", err)
			continue
		}
		now := time.Since(s.epoch)
		for _, p := range peers {
			if !p.Enhanced || p.IP == "" || p.Port == 0 {
				continue
			}
			b, err := s.bridgeFor(p)
			if err != nil {
				continue
			}
			for _, pkt := range b.tick(now) {
				if _, err := s.Server.WriteToUDP(pkt, &net.UDPAddr{IP: net.ParseIP(p.IP), Port: p.Port}); err != nil {
					slog.Error("Error sending OpenBridge keepalive", "peerID", p.ID, "error", err)
				}
			}
		}
	}
}

// encode returns the unsigned OpenBridge packet of packet for peer: DMRE
// of enhancedVersion for an enhanced peer, which keeps the source server
// and repeater of org and counts one more hop, and the 73-octet DMRD of
// version 1 for the others.
func (s *Server) encode(peer models.Peer, packet models.Packet, org origin) ([]byte, error) {
	if !peer.Enhanced {
		bridge := dmrd.Bridge{Version: 1, Data: packet.Data()}
		bridge.Data.Size = dmrconst.MMDVMPacketLength
		out, err := bridge.AppendBinary(make([]byte, 0, packetLength))
		if err != nil {
			return nil, fmt.Errorf("failed to encode OpenBridge packet: %w", err)
		}
		return out, nil
	}
	server := org.server
	if server == 0 {
		server = s.serverID
	}
	if server == 0 {
		server = uint32(peer.ID) //nolint:gosec
	}
	bridge := dmrd.Bridge{
		Version:        enhancedVersion,
		Data:           packet.Data(),
		Time:           uint64(time.Now().UnixNano()), //nolint:gosec
		SourceServer:   server,
		SourceRepeater: org.repeater,
		Hops:           org.hops + 1,
	}
	bridge.Data.Size = dmrconst.MMDVMMaxPacketLength
	out, err := bridge.AppendBinary(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to encode DMRE packet: %w", err)
	}
	return out, nil
}

// sendPacketToPeer sends a packet to a specific OpenBridge peer, from this
// replica when it holds the egress lease and from the holder otherwise.
func (s *Server) sendPacketToPeer(ctx context.Context, peerID uint, packet models.Packet, org origin) {
	if packet.Signature != string(dmrconst.CommandDMRD) {
		slog.Error("Invalid packet type", "packetType", packet.Signature)
		return
	}
	if !s.leader.Load() {
		s.handOff(peerID, packet, org)
		return
	}
	s.sendNow(ctx, peerID, packet, org)
}

func (s *Server) sendNow(ctx context.Context, peerID uint, packet models.Packet, org origin) {
	if s.isQuenched(ctx, peerID, packet.StreamID) {
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
	outbound, err := s.encode(peer, packet, org)
	if err != nil {
		slog.Error("Error encoding OpenBridge packet", "peerID", peerID, "error", err)
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
			s.handleControl(ctx, remoteAddr, data, control)
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

	var org origin
	if bridge.Version != 1 {
		org = origin{server: bridge.SourceServer, repeater: bridge.SourceRepeater, hops: bridge.Hops}
		if int(org.hops)+1 > maxHops {
			slog.Warn("Dropping an OpenBridge call that crossed too many links", "peerID", peerID, "hops", org.hops, "sourceServer", org.server, "stream", packet.StreamID)
			s.sendQuench(peer, b, bridge.Data.Destination, bridge.Data.Stream, remoteAddr)
			return
		}
	}

	should, err := rules.PeerShouldIngress(s.DB, &peer, &packet)
	if err != nil {
		slog.Error("Failed to check peer ingress rules", "peerID", peerID, "error", err)
		return
	}
	if !should {
		return
	}

	if !s.hub.AdmitStream(ctx, packet, "openbridge") {
		s.sendQuench(peer, b, bridge.Data.Destination, bridge.Data.Stream, remoteAddr)
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
			s.sendPacketToPeer(ctx, p.ID, packet, org)
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
func (s *Server) handleControl(ctx context.Context, remoteAddr *net.UDPAddr, data []byte, control dmrd.BridgeControl) {
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
		if err != nil || !b.verifiedControl(time.Since(s.epoch), data) {
			continue
		}
		if control.Command == dmrd.CommandBCSQ {
			slog.Debug("OpenBridge peer quenched a stream", "peerID", p.ID, "talkgroup", control.TalkGroup, "stream", control.Stream)
			s.quenchStream(ctx, p.ID, uint(control.Stream))
			return
		}
		slog.Debug("OpenBridge control packet", "peerID", p.ID, "command", string(control.Command), "version", control.Version)
		return
	}
	slog.Debug("Dropping an OpenBridge control packet of no known peer", "remote", remoteAddr)
}

// sendQuench asks an enhanced peer with a BCSQ not to send the stream to
// the talkgroup any more, as FreeDMR does for a call over the hop limit.
func (s *Server) sendQuench(peer models.Peer, b *bridge, talkgroup, stream uint32, to *net.UDPAddr) {
	if !peer.Enhanced || to == nil {
		return
	}
	p, err := (&dmrd.BridgeControl{Command: dmrd.CommandBCSQ, TalkGroup: talkgroup, Stream: stream}).AppendBinary(nil)
	if err == nil {
		err = b.sign(p)
	}
	if err != nil {
		slog.Error("Error making an OpenBridge source quench", "peerID", peer.ID, "error", err)
		return
	}
	if _, err := s.Server.WriteToUDP(p, to); err != nil {
		slog.Error("Error sending an OpenBridge source quench", "peerID", peer.ID, "error", err)
	}
}

func (s *Server) quenchStream(ctx context.Context, peer, stream uint) {
	now := time.Now()
	s.quenched.Store(quench{peer: peer, stream: stream}, now)
	s.shareQuench(ctx, quench{peer: peer, stream: stream})
	if s.quenched.Size() > quenchPruneSize {
		s.quenched.Range(func(q quench, at time.Time) bool {
			if now.Sub(at) > quenchTTL {
				s.quenched.Delete(q)
			}
			return true
		})
	}
}

func (s *Server) isQuenched(ctx context.Context, peer, stream uint) bool {
	at, ok := s.quenched.Load(quench{peer: peer, stream: stream})
	if !ok {
		return s.sharedQuench(ctx, quench{peer: peer, stream: stream})
	}
	if time.Since(at) > quenchTTL {
		s.quenched.Delete(quench{peer: peer, stream: stream})
		return false
	}
	return true
}
