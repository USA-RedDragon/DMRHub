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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/parrot"
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
// RepeaterID identifies the target repeater (0 means broadcast to all).
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
	// Broadcast, when true, means this server receives all group call packets
	// without per-repeater talkgroup filtering (delivered via pubsub broadcast topic).
	Broadcast bool
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
}

const serverChannelSize = 500

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
	parrot      *parrot.Parrot

	mu      sync.RWMutex
	servers map[string]*serverEntry

	subscriptionMgr *subscriptionManager

	// instanceID uniquely identifies this running instance. Used for peer
	// ownership tracking to detect when a repeater migrates to another pod.
	instanceID string

	// stopping is set when the server begins graceful shutdown. In-flight
	// calls and parrot playback continue until they finish naturally;
	// Kubernetes endpoint removal prevents new traffic from arriving.
	stopping atomic.Bool

	// callsWg tracks all in-flight activity that must complete before
	// shutdown: incoming voice calls and parrot playback goroutines.
	callsWg sync.WaitGroup

	// activeStreams tracks stream IDs currently being processed. Used to
	// distinguish the first packet of a new call (needs WG increment) from
	// continuation packets (already tracked).
	activeStreams *xsync.Map[uint, struct{}]

	// done is closed when Stop is called, allowing blocked deliverToServer
	// sends to unblock during shutdown.
	done     chan struct{}
	stopOnce sync.Once
}

// NewHub creates a new Hub.
func NewHub(db *gorm.DB, kvStore kv.KV, ps pubsub.PubSub, ct *calltracker.CallTracker) *Hub {
	h := &Hub{
		db:            db,
		kv:            kvStore,
		pubsub:        ps,
		callTracker:   ct,
		parrot:        parrot.NewParrot(kvStore),
		servers:       make(map[string]*serverEntry),
		activeStreams: xsync.NewMap[uint, struct{}](),
		done:          make(chan struct{}),
	}
	h.subscriptionMgr = newSubscriptionManager(h)
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
	}

	// Set up pubsub subscriptions for broadcast and peer servers
	if cfg.Broadcast {
		go h.subscriptionMgr.subscribeBroadcast(cfg.Name)
	}
	if cfg.Role == RolePeer {
		go h.subscriptionMgr.subscribePeers(cfg.Name)
	}

	return &ServerHandle{
		Name:    cfg.Name,
		Packets: ch,
	}
}

// UnregisterServer removes a server from the hub and closes its channel.
func (h *Hub) UnregisterServer(name string) {
	h.mu.Lock()
	defer h.mu.Unlock()

	entry, ok := h.servers[name]
	if !ok {
		return
	}
	close(entry.ch)
	delete(h.servers, name)
}

// deliverToServer pushes a routed packet to a server's channel (last-mile delivery).
// This is called by subscription manager goroutines, never as primary routing.
func (h *Hub) deliverToServer(serverName string, rp RoutedPacket) {
	h.mu.RLock()
	entry, ok := h.servers[serverName]
	h.mu.RUnlock()

	if !ok {
		return
	}

	select {
	case entry.ch <- rp:
	case <-h.done:
		slog.Debug("Hub stopping, aborting packet delivery",
			"server", serverName,
			"repeaterID", rp.RepeaterID)
	}
}

// tryDeliverToServer attempts to deliver a packet to a server's channel.
// Returns true if the packet was successfully queued, false if the server
// is not registered or the hub is stopping.
func (h *Hub) tryDeliverToServer(serverName string, rp RoutedPacket) bool {
	h.mu.RLock()
	entry, ok := h.servers[serverName]
	h.mu.RUnlock()

	if !ok {
		return false
	}

	select {
	case entry.ch <- rp:
		return true
	case <-h.done:
		return false
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

// WaitForCalls blocks until all in-flight calls and parrot playbacks
// complete. There is no timeout — the shutdown sequence waits as long as
// needed for every active call to finish.
func (h *Hub) WaitForCalls() {
	h.callsWg.Wait()
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

// ActivateRepeater sets up pubsub subscriptions for a repeater.
// Protocol servers should call this when a repeater connects.
func (h *Hub) ActivateRepeater(ctx context.Context, repeaterID uint) {
	h.claimPeerOwnership(ctx, repeaterID)
	h.activateRepeater(ctx, repeaterID)
}

// DeactivateRepeater cancels all pubsub subscriptions for a repeater.
// Protocol servers should call this when a repeater disconnects.
func (h *Hub) DeactivateRepeater(ctx context.Context, repeaterID uint) {
	h.deactivateRepeater(ctx, repeaterID)
}

// ReloadRepeater re-reads a repeater's talkgroup assignments from the database
// and adjusts subscriptions accordingly. Only reloads if the repeater is currently
// active (connected). Call this after any DB change to a repeater's talkgroup
// configuration (add/remove static TGs, link/unlink dynamic TGs).
func (h *Hub) ReloadRepeater(ctx context.Context, repeaterID uint) {
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
