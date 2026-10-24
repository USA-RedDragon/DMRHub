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

package hub

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/gorm"
)

// All DMR packets, regardless of originating protocol, flow through this hub
// for call tracking and routing. The hub is protocol-agnostic — servers register
// themselves and receive routed packets via channels.

// ServerRole describes what kind of endpoints a server manages.
type ServerRole int

const (
	// RoleRepeater is for servers managing individual repeater connections.
	// Packets are delivered per-repeater based on talkgroup subscriptions.
	RoleRepeater ServerRole = iota
	// RolePeer is for servers managing network peer connections (e.g. peering links).
	// Packets are delivered per-peer based on egress rules.
	RolePeer
)

// RoutedPacket is a packet delivered to a registered server.
// RepeaterID identifies the target repeater or peer.
type RoutedPacket struct {
	RepeaterID uint
	Packet     models.Packet
}

// ServerConfig describes a server registering with the hub.
type ServerConfig struct {
	// Name is the unique identifier for this server type (must match repeater.Type for repeater servers).
	Name string
	// Role determines the delivery model (per-repeater vs per-peer).
	Role ServerRole
}

// ServerHandle is returned by RegisterServer and provides channels for receiving packets.
type ServerHandle struct {
	Name    string
	Packets <-chan RoutedPacket
}

// serverEntry is the hub's internal record for a registered server.
type serverEntry struct {
	config ServerConfig
	ch     chan RoutedPacket
	mu     sync.RWMutex // protects ch against concurrent send-vs-close
	closed bool
	// gone is closed by UnregisterServer before it takes mu, so that a
	// send to a full channel that the server no longer reads gives up.
	gone chan struct{}
}

const serverChannelSize = 500

const (
	peerEgressQueueSize = 500
	routingQueueSize    = 500
)

// peerOwnerKeyPrefix is the KV key prefix for tracking which instance owns
// (has a live connection to) each repeater/peer. Written on ActivateRepeater,
// read during parrot playback to detect peer migration.
const peerOwnerKeyPrefix = "dmrhub:peer_owner:"

// peerOwnerTTL is the TTL for peer ownership keys. Must be long enough to
// survive brief connection interruptions but short enough to expire after a
// crash without cleanup.
const peerOwnerTTL = 5 * time.Minute

// Hub is the central routing core for all DMR protocols.
type Hub struct {
	db          *gorm.DB
	kv          kv.KV
	pubsub      pubsub.PubSub
	callTracker *calltracker.CallTracker

	mu      sync.RWMutex
	servers map[string]*serverEntry

	subscriptionMgr *subscriptionManager

	// instanceID uniquely identifies this running instance. Used for peer
	// ownership tracking to detect when a repeater migrates to another pod.
	instanceID string

	// stopping is set when the server begins graceful shutdown. In-flight
	// calls continue until they finish naturally;
	// Kubernetes endpoint removal prevents new traffic from arriving.
	stopping atomic.Bool

	// calls tracks all in-flight activity that must complete before
	// shutdown: incoming streams of every kind.
	calls inflight

	streamsMu     sync.Mutex
	activeStreams map[uint]*activeStream

	// done is closed when Stop is called, allowing blocked deliverToServer
	// sends to unblock during shutdown.
	done     chan struct{}
	stopOnce sync.Once

	peerEgress *streamorder.Queue
	routing    *streamorder.Queue

	sightings      chan sighting
	sightedStreams *xsync.Map[sightingKey, time.Time]

	undeliverable         atomic.Pointer[UndeliverableFunc]
	undeliverableLogged   *xsync.Map[uint, time.Time]
	undeliverableLogNext  atomic.Int64
	undeliverableUnlogged atomic.Uint64
	streamTargets         *xsync.Map[streamTarget, targetAt]
	streamTalkgroups      *xsync.Map[streamTarget, time.Time]
	streamPeers           *xsync.Map[streamTarget, streamPeerSet]
	streamClaims          *xsync.Map[claimKey, streamClaim]
	slotBuckets           *xsync.Map[slotKey, slotBucket]
	// slotRateOff lets the tests of the routing send faster than a slot.
	slotRateOff atomic.Bool
	loopLogged  *xsync.Map[uint, time.Time]

	repeaters *repeaterCache
	origin    string

	plugins atomic.Pointer[Plugins]

	transcoder   *transcode.Service
	transcoders  inflight
	codecStreams *xsync.Map[uint, *streamCodec]
	copyReports  *xsync.Map[copyReport, time.Time]
	tgRepeaters  *xsync.Map[uint, tgRepeaters]
	codec2Slots  atomic.Pointer[[]uint]
	heard        chan heardNote
}

// NewHub creates a new Hub.
func NewHub(db *gorm.DB, kvStore kv.KV, ps pubsub.PubSub, ct *calltracker.CallTracker) *Hub {
	h := &Hub{
		db:            db,
		kv:            kvStore,
		pubsub:        ps,
		callTracker:   ct,
		servers:       make(map[string]*serverEntry),
		activeStreams: make(map[uint]*activeStream),
		done:          make(chan struct{}),
	}
	h.peerEgress = streamorder.New(peerEgressQueueSize, h.done)
	h.routing = streamorder.New(routingQueueSize, h.done)
	h.sightings = make(chan sighting, sightingQueueSize)
	h.sightedStreams = xsync.NewMap[sightingKey, time.Time]()
	h.undeliverableLogged = xsync.NewMap[uint, time.Time]()
	h.streamTargets = xsync.NewMap[streamTarget, targetAt]()
	h.streamTalkgroups = xsync.NewMap[streamTarget, time.Time]()
	h.streamPeers = xsync.NewMap[streamTarget, streamPeerSet]()
	h.streamClaims = xsync.NewMap[claimKey, streamClaim]()
	h.slotBuckets = xsync.NewMap[slotKey, slotBucket]()
	h.loopLogged = xsync.NewMap[uint, time.Time]()
	h.transcoder = transcode.NewService(kvStore)
	h.codecStreams = xsync.NewMap[uint, *streamCodec]()
	h.copyReports = xsync.NewMap[copyReport, time.Time]()
	h.tgRepeaters = xsync.NewMap[uint, tgRepeaters]()
	h.heard = make(chan heardNote, heardQueue)
	go h.runHeard()
	go h.runSightings()
	h.subscriptionMgr = newSubscriptionManager(h)
	h.repeaters = newRepeaterCache()
	h.origin = newOrigin()
	if ps != nil {
		go h.listenRepeaterChanges(ps.Subscribe(repeaterChangedTopic))
		go h.listenCodecChanges(ps.Subscribe(codecChangedTopic))
	}
	go h.resyncRepeaters()
	if ct != nil {
		h.SetUndeliverableFunc(func(p models.Packet, target, reason string) {
			ct.Undeliverable(p, fmt.Sprintf("%s:%d:%s", target, p.Repeater, reason))
		})
	}
	return h
}

// RegisterServer registers a protocol server with the hub and returns a handle
// for receiving routed packets. The server's Name must be unique.
func (h *Hub) RegisterServer(cfg ServerConfig) *ServerHandle {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan RoutedPacket, serverChannelSize)
	h.servers[cfg.Name] = &serverEntry{
		config: cfg,
		ch:     ch,
		gone:   make(chan struct{}),
	}

	return &ServerHandle{
		Name:    cfg.Name,
		Packets: ch,
	}
}

// UnregisterServer removes a server from the hub and closes its channel.
func (h *Hub) UnregisterServer(name string) {
	h.mu.Lock()
	entry, ok := h.servers[name]
	if !ok {
		h.mu.Unlock()
		return
	}
	delete(h.servers, name)
	h.mu.Unlock()
	close(entry.gone)

	// Close the channel under the entry's write lock so that any concurrent
	// send in deliverToServer (which holds the entry read lock) finishes
	// before the channel is closed.
	entry.mu.Lock()
	entry.closed = true
	close(entry.ch)
	entry.mu.Unlock()
}

// deliverToServer pushes a routed packet to a server's channel (last-mile delivery).
// This is called by subscription manager goroutines, never as primary routing.
func (h *Hub) deliverToServer(serverName string, rp RoutedPacket) {
	h.deliverToServerUnless(nil, serverName, rp)
}

// deliverToServerUnless is deliverToServer, giving up on a full channel
// when abort is closed.
func (h *Hub) deliverToServerUnless(abort <-chan struct{}, serverName string, rp RoutedPacket) {
	h.mu.RLock()
	entry, ok := h.servers[serverName]
	h.mu.RUnlock()

	if !ok {
		return
	}

	// Hold the entry read lock during the send so that UnregisterServer
	// (which takes the write lock) cannot close the channel concurrently.
	entry.mu.RLock()
	defer entry.mu.RUnlock()

	if entry.closed {
		return
	}

	select {
	case entry.ch <- rp:
	case <-abort:
	case <-entry.gone:
	case <-h.done:
		slog.Debug("Hub stopping, aborting packet delivery",
			"server", serverName,
			"repeaterID", rp.RepeaterID)
	}
}

// getServerRole returns the role of a registered server, or -1 if not found.
func (h *Hub) getServerRole(name string) ServerRole {
	h.mu.RLock()
	defer h.mu.RUnlock()

	entry, ok := h.servers[name]
	if !ok {
		return -1
	}
	return entry.config.Role
}

// peerServers returns the names of the registered servers with RolePeer.
func (h *Hub) peerServers() []string {
	h.mu.RLock()
	defer h.mu.RUnlock()

	var names []string
	for name, entry := range h.servers {
		if entry.config.Role == RolePeer {
			names = append(names, name)
		}
	}
	return names
}

// hasPeerServers returns true if any registered server has RolePeer.
func (h *Hub) hasPeerServers() bool {
	h.mu.RLock()
	defer h.mu.RUnlock()

	for _, entry := range h.servers {
		if entry.config.Role == RolePeer {
			return true
		}
	}
	return false
}

func (h *Hub) StartDraining() {
	h.stopping.Store(true)
	slog.Info("Hub draining: waiting for in-flight calls to finish")
}

// WaitForCalls blocks until all in-flight calls complete. There is no timeout — the shutdown sequence waits as long as
// needed for every active call to finish.
func (h *Hub) WaitForCalls() {
	h.calls.Wait()
	slog.Info("All in-flight calls completed")
}

// IsStopping returns true if the hub has been told to drain.
func (h *Hub) IsStopping() bool {
	return h.stopping.Load()
}

// Stop cancels all subscriptions and signals blocked deliveries to abort (used at shutdown).
func (h *Hub) Stop(ctx context.Context) {
	h.stopOnce.Do(func() {
		slog.Info("Stopping all repeater subscriptions")
		h.stopAllRepeaters(ctx)
		close(h.done)
		h.transcoders.Wait()
		h.transcoder.Close()
	})
}

// SetInstanceID sets the unique instance identifier for this hub. Must be
// called before any repeaters are activated. The instance ID is used for
// peer ownership tracking in the shared KV store.
func (h *Hub) SetInstanceID(id string) {
	h.instanceID = id
}

// claimPeerOwnership writes an ownership record for the given repeater in
// the shared KV store. This lets other methods (e.g. parrot playback) detect
// when a peer has migrated to a different instance.
func (h *Hub) claimPeerOwnership(ctx context.Context, repeaterID uint) {
	if h.instanceID == "" {
		return
	}
	key := fmt.Sprintf("%s%d", peerOwnerKeyPrefix, repeaterID)
	if err := h.kv.Set(ctx, key, []byte(h.instanceID)); err != nil {
		slog.Warn("failed to claim peer ownership", "repeaterID", repeaterID, "error", err)
		return
	}
	if err := h.kv.Expire(ctx, key, peerOwnerTTL); err != nil {
		slog.Warn("failed to set peer ownership TTL", "repeaterID", repeaterID, "error", err)
	}
}

// isLocalPeerOwner returns true if this instance currently owns the given
// repeater according to the shared KV store. Returns true if the KV read
// fails (fail-open — direct delivery is the safe default for a single instance).
func (h *Hub) isLocalPeerOwner(ctx context.Context, repeaterID uint) bool {
	if h.instanceID == "" {
		return true // no instance tracking → always local
	}
	key := fmt.Sprintf("%s%d", peerOwnerKeyPrefix, repeaterID)
	val, err := h.kv.Get(ctx, key)
	if err != nil {
		return true // fail-open: assume local ownership
	}
	return string(val) == h.instanceID
}

// OwnsRepeater reports whether this hub owns the repeater, as
// ActivateRepeater claimed it in the KV store.
func (h *Hub) OwnsRepeater(ctx context.Context, repeaterID uint) bool {
	return h.isLocalPeerOwner(ctx, repeaterID)
}

// ActivateRepeater sets up pubsub subscriptions for a repeater.
// Protocol servers should call this when a repeater connects.
func (h *Hub) ActivateRepeater(ctx context.Context, repeaterID uint) {
	h.claimPeerOwnership(ctx, repeaterID)
	h.repeaters.invalidate(repeaterID)
	h.activateRepeater(ctx, repeaterID)
}

// DeactivateRepeater cancels all pubsub subscriptions for a repeater.
// Protocol servers should call this when a repeater disconnects.
func (h *Hub) DeactivateRepeater(ctx context.Context, repeaterID uint) {
	h.deactivateRepeater(ctx, repeaterID)
}

// ReloadRepeater re-reads a repeater from the database on every replica and
// adjusts the subscriptions of the replica where it is active. Call it after
// any database change to a repeater that routing reads: its talkgroups,
// owner, type or simplex flag, or its creation or deletion.
func (h *Hub) ReloadRepeater(ctx context.Context, repeaterID uint) {
	h.repeaterChanged(repeaterID)
	h.reloadLocal(ctx, repeaterID)
}

func (h *Hub) reloadLocal(ctx context.Context, repeaterID uint) {
	h.subscriptionMgr.mu.Lock()
	defer h.subscriptionMgr.mu.Unlock()

	// Only reload if the repeater is currently active (has been activated by a
	// protocol server on connect). This prevents leaking subscription goroutines
	// for offline repeaters when admins edit talkgroup assignments via the API.
	_, active := h.subscriptionMgr.subscriptions.Load(repeaterID)
	if !active {
		return
	}

	h.deactivateRepeaterLocked(repeaterID)
	h.activateRepeaterLocked(ctx, repeaterID)
}
