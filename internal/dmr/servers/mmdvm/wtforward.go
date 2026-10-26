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
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/quic-go/quic-go"
)

// The replicas of DMRHub share the UDP port of WebTransport behind a load
// balancer, which sends the packets of a browser to another replica when
// the one that holds its QUIC connection drains. Each replica puts a tag of
// its own at the start of the connection IDs that it gives out, and reads
// its socket through a wtConn: a short header packet with the tag of
// another replica that is alive goes to that replica over pubsub, which
// hands it to its QUIC stack under the address that the connection had,
// and sends the answers back the same way, out of the socket of the
// replica that the load balancer now uses. So a connection lives on while
// its replica drains, and the browser moves to another replica at its own
// pace.
const (
	cidLen = 12
	tagLen = 4

	wtTagKeyPrefix  = "dmrhub:wt:tag:"
	wtTagTTL        = 30 * time.Second
	wtTagHeartbeat  = 10 * time.Second
	wtForwardPrefix = "wt:fwd:"
	// wtTagsRefresh is how often a wtConn reads the tags of the replicas
	// that run.
	wtTagsRefresh = time.Second
	// wtMaxEntries is the most connections and paths that a wtConn keeps,
	// so that a flood of made-up connection IDs cannot fill its memory.
	wtMaxEntries = 4096
	// wtPathIdle is how long a wtConn keeps what it learned of a
	// connection after the last packet of it.
	wtPathIdle = time.Minute
	// wtQueue is how many packets a wtConn queues for its QUIC stack.
	wtQueue = 1024
)

const (
	forwardIn  byte = 1
	forwardOut byte = 2
)

type replicaTag [tagLen]byte

func (t replicaTag) String() string {
	return hex.EncodeToString(t[:])
}

func newReplicaTag() (replicaTag, error) {
	var t replicaTag
	if _, err := rand.Read(t[:]); err != nil {
		return t, fmt.Errorf("making a replica tag: %w", err)
	}
	return t, nil
}

// cidGenerator makes the connection IDs of a replica: its tag and random
// octets.
type cidGenerator struct {
	tag replicaTag
}

func (g cidGenerator) GenerateConnectionID() (quic.ConnectionID, error) {
	var b [cidLen]byte
	copy(b[:], g.tag[:])
	if _, err := rand.Read(b[tagLen:]); err != nil {
		return quic.ConnectionID{}, fmt.Errorf("making a connection ID: %w", err)
	}
	return quic.ConnectionIDFromBytes(b[:]), nil
}

func (g cidGenerator) ConnectionIDLen() int {
	return cidLen
}

type datagram struct {
	data []byte
	addr net.Addr
}

// path is where the packets of a connection go now: out of the socket of
// the replica via, to the address to.
type path struct {
	via  replicaTag
	to   *net.UDPAddr
	seen time.Time
}

type origin struct {
	addr net.Addr
	seen time.Time
}

// wtConn is the UDP socket of WebTransport as the QUIC stack of a replica
// reads and writes it, with the packets of the connections of other
// replicas forwarded to them and theirs forwarded here.
type wtConn struct {
	udp *net.UDPConn
	tag replicaTag
	ps  pubsub.PubSub
	kv  kv.KV
	sub pubsub.Subscription

	in   chan datagram
	done chan struct{}
	once sync.Once
	wg   sync.WaitGroup

	deadlineMu sync.Mutex
	deadline   time.Time
	kick       chan struct{}

	// tags are the tags of the replicas that run, which heartbeat reads,
	// so that the reader never waits on the KV store.
	tags atomic.Pointer[map[replicaTag]bool]

	// onPath, when set, takes the address that the QUIC stack knows a
	// connection of this replica by when its packets begin to come another
	// way, and the time of it.
	onPath atomic.Pointer[func(net.Addr, time.Time)]

	mu      sync.Mutex
	origins map[string]origin
	paths   map[string]path
	pruned  time.Time
}

// newWTConn reads udp for the QUIC stack of the replica with the tag. With
// a nil ps or kv it forwards nothing, as a lone instance.
func newWTConn(udp *net.UDPConn, tag replicaTag, ps pubsub.PubSub, store kv.KV) *wtConn {
	c := &wtConn{
		udp:     udp,
		tag:     tag,
		ps:      ps,
		kv:      store,
		in:      make(chan datagram, wtQueue),
		done:    make(chan struct{}),
		kick:    make(chan struct{}, 1),
		origins: map[string]origin{},
		paths:   map[string]path{},
	}
	empty := map[replicaTag]bool{}
	c.tags.Store(&empty)
	if ps != nil && store != nil {
		c.register()
		c.readTags()
		c.sub = ps.Subscribe(wtForwardPrefix + tag.String())
		c.wg.Add(2)
		go c.heartbeat()
		go c.forwarded()
	}
	c.wg.Add(1)
	go c.read()
	return c
}

func (c *wtConn) register() {
	ctx := context.Background()
	key := wtTagKeyPrefix + c.tag.String()
	if err := c.kv.Set(ctx, key, []byte{1}); err != nil {
		slog.Error("Error registering the WebTransport tag of the replica", "error", err)
		return
	}
	if err := c.kv.Expire(ctx, key, wtTagTTL); err != nil {
		slog.Error("Error setting the time of the WebTransport tag of the replica", "error", err)
	}
}

func (c *wtConn) heartbeat() {
	defer c.wg.Done()
	t := time.NewTicker(wtTagHeartbeat)
	defer t.Stop()
	tags := time.NewTicker(wtTagsRefresh)
	defer tags.Stop()
	for {
		select {
		case <-c.done:
			return
		case <-t.C:
			c.register()
		case <-tags.C:
			c.readTags()
		}
	}
}

// readTags reads the tags of the replicas that run.
func (c *wtConn) readTags() {
	ctx := context.Background()
	out := map[replicaTag]bool{}
	var cursor uint64
	for {
		keys, next, err := c.kv.Scan(ctx, cursor, wtTagKeyPrefix+"*", 1000)
		if err != nil {
			slog.Warn("Error reading the WebTransport tags of the replicas", "error", err)
			return
		}
		for _, k := range keys {
			b, err := hex.DecodeString(strings.TrimPrefix(k, wtTagKeyPrefix))
			if err != nil || len(b) != tagLen {
				continue
			}
			var t replicaTag
			copy(t[:], b)
			out[t] = true
		}
		if next == 0 {
			break
		}
		cursor = next
	}
	c.tags.Store(&out)
}

// read takes the packets of the socket: those of the connections of this
// replica, and new ones, go to the QUIC stack, and those of another
// replica that is alive go to it.
func (c *wtConn) read() {
	defer c.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, addr, err := c.udp.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		b := append([]byte(nil), buf[:n]...)
		owner, ok, back := c.foreign(b, addr)
		if back {
			c.pathMoved(addr, time.Now())
		}
		if ok {
			c.forward(owner, forwardIn, addr, b)
			continue
		}
		select {
		case c.in <- datagram{data: b, addr: addr}:
		case <-c.done:
			return
		}
	}
}

// foreign reports the replica whose connection the packet b is for when
// that is another replica that is alive, and notes where the connections
// of this one are. It reports back when b is of a connection of this one
// that came through another replica until now.
func (c *wtConn) foreign(b []byte, addr *net.UDPAddr) (owner replicaTag, ok, back bool) {
	if len(b) < 1+cidLen || b[0]&0x80 != 0 {
		return replicaTag{}, false, false
	}
	copy(owner[:], b[1:1+tagLen])
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.prune(now)
	if owner == c.tag {
		key := string(b[1 : 1+cidLen])
		if _, known := c.origins[key]; known || len(c.origins) < wtMaxEntries {
			c.origins[key] = origin{addr: addr, seen: now}
		}
		_, back = c.paths[addr.String()]
		delete(c.paths, addr.String())
		return replicaTag{}, false, back
	}
	if c.ps == nil || c.kv == nil {
		return replicaTag{}, false, false
	}
	return owner, (*c.tags.Load())[owner], false
}

// prune forgets the connections and paths that were idle for wtPathIdle.
// The caller holds c.mu.
func (c *wtConn) prune(now time.Time) {
	if now.Sub(c.pruned) < wtPathIdle {
		return
	}
	c.pruned = now
	for k, o := range c.origins {
		if now.Sub(o.seen) > wtPathIdle {
			delete(c.origins, k)
		}
	}
	for k, p := range c.paths {
		if now.Sub(p.seen) > wtPathIdle {
			delete(c.paths, k)
		}
	}
}

// forward sends the packet b to the replica to: a packet from addr for its
// QUIC stack, or one for it to send to addr.
func (c *wtConn) forward(to replicaTag, kind byte, addr *net.UDPAddr, b []byte) {
	a := addr.String()
	if len(a) > 255 {
		return
	}
	msg := make([]byte, 0, 2+tagLen+len(a)+len(b))
	msg = append(msg, kind)
	msg = append(msg, c.tag[:]...)
	msg = append(msg, byte(len(a))) //nolint:gosec // len(a) is at most 255, as checked above
	msg = append(msg, a...)
	msg = append(msg, b...)
	if err := c.ps.Publish(wtForwardPrefix+to.String(), msg); err != nil {
		slog.Warn("Error forwarding a WebTransport packet to another replica", "error", err)
	}
}

// forwarded takes the packets that other replicas forward here.
func (c *wtConn) forwarded() {
	defer c.wg.Done()
	defer func() { _ = c.sub.Close() }()
	ch := c.sub.Channel()
	for {
		select {
		case <-c.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			c.take(msg)
		}
	}
}

func (c *wtConn) take(msg []byte) {
	if len(msg) < 2+tagLen {
		return
	}
	kind := msg[0]
	var from replicaTag
	copy(from[:], msg[1:1+tagLen])
	n := int(msg[1+tagLen])
	rest := msg[2+tagLen:]
	if len(rest) < n {
		return
	}
	addr, err := net.ResolveUDPAddr("udp", string(rest[:n]))
	if err != nil {
		return
	}
	b := rest[n:]
	switch kind {
	case forwardOut:
		if _, err := c.udp.WriteToUDP(b, addr); err != nil {
			slog.Debug("Error sending a forwarded WebTransport packet", "error", err)
		}
	case forwardIn:
		known, moved := c.pathFrom(from, addr, b)
		if moved {
			c.pathMoved(known, time.Now())
		}
		select {
		case c.in <- datagram{data: b, addr: known}:
		case <-c.done:
		}
	}
}

// pathMoved hands addr, the address of a connection whose packets began
// to come another way at at, to onPath.
func (c *wtConn) pathMoved(addr net.Addr, at time.Time) {
	if fn := c.onPath.Load(); fn != nil {
		(*fn)(addr, at)
	}
}

// pathFrom notes that the connection of the packet b now comes through the
// replica from, from addr, and returns the address that the QUIC stack
// knows the connection by, so that it sees no new path, and whether the
// connection came another way until now.
func (c *wtConn) pathFrom(from replicaTag, addr *net.UDPAddr, b []byte) (net.Addr, bool) {
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	known := net.Addr(addr)
	if len(b) >= 1+cidLen {
		if o, ok := c.origins[string(b[1:1+cidLen])]; ok {
			known = o.addr
			c.origins[string(b[1:1+cidLen])] = origin{addr: o.addr, seen: now}
		}
	}
	prev, ok := c.paths[known.String()]
	if !ok && len(c.paths) >= wtMaxEntries {
		return known, false
	}
	c.paths[known.String()] = path{via: from, to: addr, seen: now}
	return known, !ok || prev.via != from || prev.to.String() != addr.String()
}

// deadlineError is the error of a read past the read deadline. It is
// temporary, as the QUIC stack sets a deadline to wake its reader.
type deadlineError struct{}

func (deadlineError) Error() string   { return "i/o timeout" }
func (deadlineError) Timeout() bool   { return true }
func (deadlineError) Temporary() bool { return true }

func (c *wtConn) ReadFrom(p []byte) (int, net.Addr, error) {
	for {
		c.deadlineMu.Lock()
		deadline := c.deadline
		c.deadlineMu.Unlock()
		var expired <-chan time.Time
		if !deadline.IsZero() {
			wait := time.Until(deadline)
			if wait <= 0 {
				return 0, nil, deadlineError{}
			}
			t := time.NewTimer(wait)
			expired = t.C
			defer t.Stop()
		}
		select {
		case d := <-c.in:
			return copy(p, d.data), d.addr, nil
		case <-c.done:
			return 0, nil, net.ErrClosed
		case <-expired:
			return 0, nil, deadlineError{}
		case <-c.kick:
		}
	}
}

func (c *wtConn) WriteTo(p []byte, addr net.Addr) (int, error) {
	c.mu.Lock()
	pt, ok := c.paths[addr.String()]
	c.mu.Unlock()
	if ok {
		c.forward(pt.via, forwardOut, pt.to, p)
		return len(p), nil
	}
	n, err := c.udp.WriteTo(p, addr)
	if err != nil {
		return n, fmt.Errorf("sending a WebTransport packet: %w", err)
	}
	return n, nil
}

// unregister removes the tag of the replica from the KV store, so that the
// other replicas stop forwarding to it.
func (c *wtConn) unregister(ctx context.Context) {
	if c.kv == nil || c.ps == nil {
		return
	}
	if err := c.kv.Delete(ctx, wtTagKeyPrefix+c.tag.String()); err != nil {
		slog.Warn("Error removing the WebTransport tag of the replica", "error", err)
	}
}

func (c *wtConn) Close() error {
	var err error
	c.once.Do(func() {
		close(c.done)
		if cerr := c.udp.Close(); cerr != nil {
			err = fmt.Errorf("closing the WebTransport socket: %w", cerr)
		}
		c.wg.Wait()
	})
	return err
}

func (c *wtConn) LocalAddr() net.Addr { return c.udp.LocalAddr() }

// SetDeadline sets the read deadline; writes do not block.
func (c *wtConn) SetDeadline(t time.Time) error {
	return c.SetReadDeadline(t)
}

// SetReadDeadline sets the time after which ReadFrom fails with a
// temporary timeout, and wakes a ReadFrom that waits, as the QUIC stack
// does to stop its reader. The zero time waits for ever.
func (c *wtConn) SetReadDeadline(t time.Time) error {
	c.deadlineMu.Lock()
	c.deadline = t
	c.deadlineMu.Unlock()
	select {
	case c.kick <- struct{}{}:
	default:
	}
	return nil
}

// SetWriteDeadline does nothing, as the writes do not block.
func (c *wtConn) SetWriteDeadline(time.Time) error { return nil }

func (c *wtConn) SetReadBuffer(n int) error {
	if err := c.udp.SetReadBuffer(n); err != nil {
		return fmt.Errorf("setting the read buffer: %w", err)
	}
	return nil
}

func (c *wtConn) SetWriteBuffer(n int) error {
	if err := c.udp.SetWriteBuffer(n); err != nil {
		return fmt.Errorf("setting the write buffer: %w", err)
	}
	return nil
}

var _ net.PacketConn = (*wtConn)(nil)
