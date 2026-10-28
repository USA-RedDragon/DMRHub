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
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/streamorder"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
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

// Option sets up a Hub in NewHub.
type Option func(*Hub)

// WithClock makes the hub, and the servers that ask it for its clock, wait
// and measure on c.
func WithClock(c clock.Clock) Option {
	return func(h *Hub) { h.clock = clock.Or(c) }
}

// Clock returns the clock of the hub, or the real clock for a nil hub.
func (h *Hub) Clock() clock.Clock {
	if h == nil {
		return clock.Real()
	}
	return h.clock
}

// RoutedPacket is a packet delivered to a registered server.
// RepeaterID identifies the target repeater or peer.
type RoutedPacket struct {
	RepeaterID uint
	Packet     models.Packet
	// Barrier, when set, makes this a marker and no packet: the server
	// closes it when it gets to it. See Hub.Barrier.
	Barrier chan struct{}
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
// read by OwnsRepeater and OwnerLeaving and when a replica drains.
const peerOwnerKeyPrefix = "dmrhub:peer_owner:"

// peerOwnerTTL is the TTL for peer ownership keys. Must be long enough to
// survive brief connection interruptions but short enough to expire after a
// crash without cleanup.
const peerOwnerTTL = 5 * time.Minute

// laterKey names a queue of the background work of the hub that goes to
// Redis: what it is, and the radio or repeater it is for.
type laterKey struct {
	what string
	id   uint
}

// sightingsWork is the work of laterKey that records and tells where a
// radio was heard.
const sightingsWork = "sightings"

// Hub is the central routing core for all DMR protocols.
type Hub struct {
	clock       clock.Clock
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

	peerEgress *streamorder.Queue[sourceSlot]
	routing    *streamorder.Queue[sourceSlot]
	links      *streamorder.Queue[uint]
	outbox     *outbox.Outbox[sourceSlot]
	later      *outbox.Outbox[laterKey]

	talkgroupIDs *lookup.Cache[uint, bool]
	// talkgroups holds the IDs of all talkgroups, so that a burst to one
	// never waits on the database.
	talkgroups   atomic.Pointer[map[uint]struct{}]
	talkgroupsMu sync.Mutex
	radioOwners  *lookup.Cache[uint, uint]
	radioCalls   *lookup.Cache[uint, heardAt]
	egressTable  *lookup.Cache[struct{}, []egressPeer]
	rules        atomic.Pointer[ruleTable]
	rulesMu      sync.Mutex
	lastSeen     *lookup.Flights[uint, []byte]

	sightings      chan sighting
	sightedStreams *xsync.Map[sightingKey, time.Time]
	heardOn        *xsync.Map[uint, heardAt]

	undeliverable         atomic.Pointer[UndeliverableFunc]
	undeliverableLogged   *xsync.Map[uint, time.Time]
	undeliverableLogNext  atomic.Int64
	undeliverableUnlogged atomic.Uint64
	streamTargets         *xsync.Map[streamTarget, targetAt]
	streamTalkgroups      *xsync.Map[streamTarget, time.Time]
	streamPeers           *xsync.Map[streamTarget, streamPeerSet]
	streamClaims          *xsync.Map[claimKey, streamClaim]
	claimWaits            *xsync.Map[sourceSlot, *claimWait]
	claimTiming           atomic.Pointer[[2]time.Duration]
	claimWaitEnd          atomic.Pointer[chan time.Time]
	slotBuckets           *xsync.Map[slotKey, slotBucket]
	// slotRateOff lets the tests of the routing send faster than a slot.
	slotRateOff       atomic.Bool
	loopLogged        *xsync.Map[uint, time.Time]
	privateDropLogged *xsync.Map[uint, time.Time]
	cfg               atomic.Pointer[config.Source]
	configSet         chan struct{}
	dynamicUses       *xsync.Map[repeaterSlot, dynamicUse]
	slotHolders       *xsync.Map[repeaterSlot, slotHolder]
	slotRefused       *xsync.Map[slotStream, time.Time]
	streamPolicies    *xsync.Map[claimKey, streamPolicy]
	autoTalkgroups    *xsync.Map[uint, struct{}]
	talkgroupCalls    *xsync.Map[uint, time.Time]

	repeaters *repeaterCache
	origin    string
	watchers  watchers

	plugins        atomic.Pointer[Plugins]
	positionNotify atomic.Pointer[PositionNotify]

	ipListsMu sync.Mutex
	ipLists   atomic.Pointer[ipLists]

	transcoder   *transcode.Service
	transcoders  inflight
	codecStreams *xsync.Map[uint, *streamCodec]
	copyMu       sync.Mutex
	copyOrders   map[sourceSlot]*copyOrder
	copyReports  *xsync.Map[copyReport, time.Time]
	tgRepeaters  *lookup.Cache[uint, []uint]
	codec2Slots  atomic.Pointer[[]uint]
	heard        chan heardNote
	// left is signalled when a repeater leaves this hub, for WaitMoved.
	left chan struct{}

	followClaimsOnce sync.Once
	// handing counts the claims of other replicas whose callbacks run.
	handing     inflight
	claimedMu   sync.Mutex
	claimed     map[int]func(repeaterID uint, since time.Time)
	claimedNext int
	// claimedAt is the time of the last claim of each repeater that this
	// replica made, set before the repeater is active here.
	claimedAt *xsync.Map[uint, time.Time]
	// claimedElsewhere is the time of the last claim of each repeater
	// that another replica made, as this replica heard it.
	claimedElsewhere *xsync.Map[uint, time.Time]
	owners           *lookup.Flights[uint, []byte]
}

// NewHub creates a new Hub.
func NewHub(db *gorm.DB, kvStore kv.KV, ps pubsub.PubSub, ct *calltracker.CallTracker, opts ...Option) *Hub {
	h := &Hub{
		clock:         clock.Real(),
		db:            db,
		kv:            kvStore,
		pubsub:        ps,
		callTracker:   ct,
		servers:       make(map[string]*serverEntry),
		activeStreams: make(map[uint]*activeStream),
		done:          make(chan struct{}),
		left:          make(chan struct{}, 1),
		configSet:     make(chan struct{}, 1),
	}
	for _, opt := range opts {
		opt(h)
	}
	h.peerEgress = streamorder.New[sourceSlot](peerEgressQueueSize, h.done)
	h.routing = streamorder.New[sourceSlot](routingQueueSize, h.done)
	h.links = streamorder.New[uint](routingQueueSize, h.done)
	h.outbox = outbox.New(ps, h.done, "the bursts of a source slot", func(k sourceSlot) []any {
		return []any{"server", k.server, "repeater", k.repeater, "slot", k.slot}
	})
	h.later = outbox.New(ps, h.done, "the background work of the hub", func(k laterKey) []any {
		return []any{"work", k.what, "id", k.id}
	})
	h.autoTalkgroups = xsync.NewMap[uint, struct{}]()
	h.talkgroupCalls = xsync.NewMap[uint, time.Time]()
	h.talkgroupIDs = lookup.New(h.clock, "talkgroups", lookup.TTL, func(id uint) (bool, error) { return models.TalkgroupIDExists(h.db, id) })
	h.radioOwners = lookup.New(h.clock, "radio owners", lookup.TTL, h.radioOwner)
	h.radioCalls = lookup.New(h.clock, "radio calls", lookup.TTL, h.lastCallRepeater)
	h.egressTable = lookup.New(h.clock, "peers", lookup.TTL, h.loadEgressPeers)
	h.lastSeen = lookup.NewFlights[uint, []byte](h.clock)
	h.sightings = make(chan sighting, sightingQueueSize)
	h.sightedStreams = xsync.NewMap[sightingKey, time.Time]()
	h.heardOn = xsync.NewMap[uint, heardAt]()
	h.undeliverableLogged = xsync.NewMap[uint, time.Time]()
	h.streamTargets = xsync.NewMap[streamTarget, targetAt]()
	h.streamTalkgroups = xsync.NewMap[streamTarget, time.Time]()
	h.streamPeers = xsync.NewMap[streamTarget, streamPeerSet]()
	h.streamClaims = xsync.NewMap[claimKey, streamClaim]()
	h.claimWaits = xsync.NewMap[sourceSlot, *claimWait]()
	h.claimedAt = xsync.NewMap[uint, time.Time]()
	h.claimedElsewhere = xsync.NewMap[uint, time.Time]()
	h.owners = lookup.NewFlights[uint, []byte](h.clock)
	h.slotBuckets = xsync.NewMap[slotKey, slotBucket]()
	h.loopLogged = xsync.NewMap[uint, time.Time]()
	h.privateDropLogged = xsync.NewMap[uint, time.Time]()
	h.dynamicUses = xsync.NewMap[repeaterSlot, dynamicUse]()
	h.slotHolders = xsync.NewMap[repeaterSlot, slotHolder]()
	h.slotRefused = xsync.NewMap[slotStream, time.Time]()
	h.streamPolicies = xsync.NewMap[claimKey, streamPolicy]()
	h.transcoder = transcode.NewService(kvStore, h.clock)
	h.codecStreams = xsync.NewMap[uint, *streamCodec]()
	h.copyOrders = map[sourceSlot]*copyOrder{}
	h.copyReports = xsync.NewMap[copyReport, time.Time]()
	h.tgRepeaters = lookup.New(h.clock, "talkgroup repeaters", tgRepeatersTTL, func(tg uint) ([]uint, error) { return models.TalkgroupRepeaterIDs(h.db, tg) })
	h.heard = make(chan heardNote, heardQueue)
	if db != nil {
		h.loadRules()
		h.loadTalkgroups()
		h.loadPeers()
	}
	go h.runHeard()
	go h.runSightings()
	h.subscriptionMgr = newSubscriptionManager(h)
	go h.runDynamicTimeouts()
	go h.runEphemeralTalkgroups()
	h.repeaters = newRepeaterCache(h.clock)
	h.origin = newOrigin()
	if ps != nil {
		go h.listenRepeaterChanges(ps.Subscribe(repeaterChangedTopic))
		go h.listenCodecChanges(ps.Subscribe(codecChangedTopic))
		for _, topic := range []string{talkgroupChangedTopic, userChangedTopic, peerChangedTopic, repeaterRekeyTopic, rulesChangedTopic} {
			go h.listenChanges(topic, ps.Subscribe(topic))
		}
	}
	go h.resyncRepeaters()
	if ct != nil {
		h.SetUndeliverableFunc(func(p models.Packet, target, reason string) {
			ct.Undeliverable(p, undeliverable.Sentence(target, p.Repeater, reason))
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

// WaitForCalls blocks until all in-flight calls complete and pubsub took
// their bursts. There is no timeout — the shutdown sequence waits as long as
// needed for every active call to finish.
func (h *Hub) WaitForCalls() {
	h.calls.Wait()
	h.outbox.Wait()
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

// ErrServersStarted is returned by SetInstanceID when a server already
// uses the hub.
var ErrServersStarted = errors.New("the instance ID must be set before the servers start")

// SetInstanceID sets the unique instance identifier for this hub. The
// instance ID is used for peer ownership tracking in the shared KV store.
// The servers read it without a lock, so it returns ErrServersStarted and
// changes nothing once a server has registered with the hub.
func (h *Hub) SetInstanceID(id string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.servers) > 0 {
		return ErrServersStarted
	}
	h.instanceID = id
	if id != "" && h.pubsub != nil {
		h.followClaimsOnce.Do(func() { go h.followClaims(h.pubsub.Subscribe(claimedTopic)) })
	}
	return nil
}

// claimPeerOwnership writes an ownership record for the given repeater in
// the shared KV store and tells the other replicas, so that a replica that
// served the repeater before ends its session there.
func (h *Hub) claimPeerOwnership(ctx context.Context, repeaterID uint, since time.Time) {
	if h.writeOwner(ctx, repeaterID) {
		h.publishClaim(repeaterID, since)
	}
}

// RenewOwnership keeps the ownership record of a repeater that this hub
// serves alive. The servers call it when the repeater shows that it is
// still here, such as at a ping, since the record expires after
// peerOwnerTTL.
func (h *Hub) RenewOwnership(ctx context.Context, repeaterID uint) {
	h.writeOwner(ctx, repeaterID)
}

func (h *Hub) writeOwner(ctx context.Context, repeaterID uint) bool {
	if h.instanceID == "" {
		return false
	}
	key := ownerKey(repeaterID)
	if err := h.kv.Set(ctx, key, []byte(h.instanceID)); err != nil {
		slog.Warn("failed to claim peer ownership", "repeaterID", repeaterID, "error", err)
		return false
	}
	if err := h.kv.Expire(ctx, key, peerOwnerTTL); err != nil {
		slog.Warn("failed to set peer ownership TTL", "repeaterID", repeaterID, "error", err)
	}
	return true
}

// OwnsRepeater reports whether this hub owns the repeater, as
// ActivateRepeater claimed it in the KV store. Without an instance ID, as
// a lone instance, it owns every repeater. It waits for the KV store at
// most lookup.Wait, as for the database. When the KV store does not
// answer in that time, or fails, it answers from the claims that this hub
// heard: it owns the repeater unless another replica claimed it after the
// last claim of this one.
func (h *Hub) OwnsRepeater(ctx context.Context, repeaterID uint) bool {
	if h.instanceID == "" {
		return true
	}
	read := context.WithoutCancel(ctx)
	val, err := h.owners.Do(repeaterID, func() ([]byte, error) { return h.kv.Get(read, ownerKey(repeaterID)) }) //nolint:wrapcheck
	if err == nil {
		return string(val) == h.instanceID
	}
	other, ok := h.claimedElsewhere.Load(repeaterID)
	if !ok {
		return true
	}
	mine, ok := h.claimedAt.Load(repeaterID)
	return ok && other.Before(mine)
}

// ActivateRepeater sets up pubsub subscriptions for a repeater.
// Protocol servers should call this when a repeater connects.
// It subscribes before it claims the repeater, so that the replica that
// served the repeater before keeps sending to it until this one can.
func (h *Hub) ActivateRepeater(ctx context.Context, repeaterID uint) {
	h.ActivateRepeaterSince(ctx, repeaterID, h.clock.Now())
}

// ActivateRepeaterSince is ActivateRepeater for a repeater whose packet
// that the server got at since made it active. The claim tells the
// replica that served the repeater before since, so that it can hand over
// what it sent after it, which a repeater that moved never got.
func (h *Hub) ActivateRepeaterSince(ctx context.Context, repeaterID uint, since time.Time) {
	h.claimedAt.Store(repeaterID, since)
	h.reloadRepeater(repeaterID)
	h.activateRepeater(ctx, repeaterID)
	h.claimPeerOwnership(ctx, repeaterID, since)
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

	h.resubscribeLocked(ctx, repeaterID)
}
