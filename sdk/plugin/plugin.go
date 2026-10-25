// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

// Package plugin connects a DMRHub plugin to its hub. A plugin answers the
// calls to the DMR IDs an admin gave it, and sends calls only from those IDs.
//
//	plugin.Run(ctx, plugin.Options{Address: "hub.example.org:3006", Token: token, Fingerprint: fp},
//		func(ctx context.Context, conn *plugin.Conn) error {
//			for {
//				m, err := conn.Recv()
//				if err != nil {
//					return err
//				}
//				...
//				err = conn.Send(reply)
//			}
//		})
package plugin

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

// Options says how to reach the hub.
type Options struct {
	// Address is the hub's plugin listener, as host:port.
	Address string
	// Token is the plugin's token, from the hub's Plugins page.
	Token string
	// Fingerprint is the SHA-256 of the hub's own certificate, as shown on
	// the Plugins page. Leave it empty when the hub has a certificate from a
	// certificate authority.
	Fingerprint string
}

// ErrFingerprint is returned when the hub's certificate does not match the
// fingerprint.
var ErrFingerprint = errors.New("the hub's certificate does not match the fingerprint")

// ErrNoWelcome is returned when the hub does not start with a Welcome.
var ErrNoWelcome = errors.New("the hub did not welcome the plugin")

// pingEvery is how often a quiet connection checks that the hub is there.
const pingEvery = 30 * time.Second

const (
	// moveFor is how long a plugin tries to connect again after a Draining,
	// about as long as a replica drains.
	moveFor = 30 * time.Second
	// moveEvery is the wait between those tries.
	moveEvery = 200 * time.Millisecond
	// sameBurstFor is how long a burst from one stream is kept to drop the
	// same burst from another stream.
	sameBurstFor = 5 * time.Second
)

// ErrClosed is returned by Recv after Close.
var ErrClosed = errors.New("the connection is closed")

// Conn is a plugin's connection to its hub. When the replica of the hub
// that holds it drains, Conn connects again on its own and moves to the
// new stream, with no error or Welcome for the plugin.
type Conn struct {
	dial    func(wait time.Duration) (*leg, error)
	stop    context.CancelFunc
	welcome *pluginv1.Welcome
	events  chan event
	done    chan struct{}
	once    sync.Once

	mu     sync.Mutex
	cur    *leg
	legs   []*leg
	moving bool
	failed error
	seen   map[burstKey]seenBurst
	pruned time.Time

	sendMu   sync.Mutex
	tracking bool

	moveFor   time.Duration
	moveEvery time.Duration
}

// leg is one stream to the hub, on its own connection.
type leg struct {
	conn    *grpc.ClientConn
	stream  pluginv1.PluginService_ConnectClient
	cancel  context.CancelFunc
	welcome *pluginv1.Welcome
	once    sync.Once
	ended   error
}

func (l *leg) close() {
	l.once.Do(func() {
		l.cancel()
		_ = l.conn.Close()
	})
}

// event is a message or the end of a leg, or the end of a move.
type event struct {
	leg   *leg
	msg   *pluginv1.ConnectResponse
	err   error
	moved bool
}

type burstKey struct {
	stream, sequence, slot, source uint32
}

type seenBurst struct {
	leg *leg
	at  time.Time
}

func normalize(fp string) string {
	return strings.ToLower(strings.NewReplacer(":", "", " ", "").Replace(fp))
}

func tlsConfig(fingerprint string) *tls.Config {
	if fingerprint == "" {
		return &tls.Config{MinVersion: tls.VersionTLS13}
	}
	want := normalize(fingerprint)
	return &tls.Config{
		MinVersion: tls.VersionTLS13,
		// The certificate is checked by its fingerprint instead of a
		// certificate authority.
		InsecureSkipVerify: true, //nolint:gosec // checked by VerifyConnection
		VerifyConnection: func(cs tls.ConnectionState) error {
			if len(cs.PeerCertificates) == 0 {
				return ErrFingerprint
			}
			sum := sha256.Sum256(cs.PeerCertificates[0].Raw)
			if hex.EncodeToString(sum[:]) != want {
				return ErrFingerprint
			}
			return nil
		},
	}
}

// Dial connects to the hub and waits for its Welcome. The connection lasts
// until ctx ends or Close.
func Dial(ctx context.Context, o Options) (*Conn, error) {
	ctx, stop := context.WithCancel(ctx)
	dial := func(wait time.Duration) (*leg, error) { return dialLeg(ctx, o, wait) }
	first, err := dial(0)
	if err != nil {
		stop()
		return nil, err
	}
	c := &Conn{
		dial: dial, stop: stop, welcome: first.welcome, events: make(chan event), done: make(chan struct{}),
		cur: first, legs: []*leg{first}, seen: map[burstKey]seenBurst{}, moveFor: moveFor, moveEvery: moveEvery,
	}
	go c.read(first)
	return c, nil
}

// dialLeg opens a stream and waits for its Welcome, for at most wait unless
// wait is 0.
func dialLeg(ctx context.Context, o Options, wait time.Duration) (*leg, error) {
	conn, err := grpc.NewClient(o.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig(o.Fingerprint))),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: pingEvery, Timeout: pingEvery, PermitWithoutStream: true}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not reach the hub: %w", err)
	}
	ctx, cancel := context.WithCancel(metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+o.Token))
	l := &leg{conn: conn, cancel: cancel}
	l.stream, err = pluginv1.NewPluginServiceClient(conn).Connect(ctx)
	if err != nil {
		l.close()
		return nil, fmt.Errorf("could not connect to the hub: %w", err)
	}
	if wait > 0 {
		t := time.AfterFunc(wait, cancel)
		defer t.Stop()
	}
	first, err := l.stream.Recv()
	if err != nil {
		l.close()
		return nil, fmt.Errorf("could not connect to the hub: %w", err)
	}
	if first.GetWelcome() == nil {
		l.close()
		return nil, ErrNoWelcome
	}
	l.welcome = first.GetWelcome()
	return l, nil
}

// read hands the messages of l to Recv until l ends.
func (c *Conn) read(l *leg) {
	for {
		m, err := l.stream.Recv()
		select {
		case c.events <- event{leg: l, msg: m, err: err}:
		case <-c.done:
			return
		}
		if err != nil {
			return
		}
	}
}

// Name returns the plugin's name on the hub.
func (c *Conn) Name() string {
	return c.welcome.GetName()
}

// Ranges returns the plugin's IDs when it connected.
func (c *Conn) Ranges() []*pluginv1.IDRange {
	return c.welcome.GetRanges()
}

// Recv returns the next message from the hub: a burst to one of the
// plugin's IDs, a refusal of one of its bursts, its new IDs, or what it
// subscribed to. Call it from one goroutine at a time.
func (c *Conn) Recv() (*pluginv1.ConnectResponse, error) {
	for {
		if c.failed != nil {
			return nil, c.failed
		}
		var e event
		select {
		case <-c.done:
			return nil, ErrClosed
		case e = <-c.events:
		}
		if m, ok := c.handle(e); ok {
			return m, nil
		}
	}
}

// handle returns the message of e for Recv, or false when Recv waits for
// more. It sets failed when the connection ended.
func (c *Conn) handle(e event) (*pluginv1.ConnectResponse, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case e.moved:
		c.moving = false
		if c.cur.ended != nil {
			c.failed = fmt.Errorf("the hub closed the connection: %w", c.cur.ended)
		}
		return nil, false
	case e.err != nil:
		e.leg.ended = e.err
		e.leg.close()
		c.legs = slices.DeleteFunc(c.legs, func(l *leg) bool { return l == e.leg })
		if e.leg == c.cur && !c.moving {
			c.failed = fmt.Errorf("the hub closed the connection: %w", e.err)
		}
		return nil, false
	case e.msg.GetDraining() != nil:
		if e.leg == c.cur && !c.moving {
			c.moving = true
			go c.move()
		}
		return nil, false
	case e.msg.GetBurst() != nil:
		if c.sameBurst(e.leg, e.msg.GetBurst()) {
			return nil, false
		}
	}
	return e.msg, true
}

// sameBurst reports whether another leg gave b a short time ago. While the
// plugin moves to a new stream, and while it has two, it keeps b.
func (c *Conn) sameBurst(l *leg, b *pluginv1.Burst) bool {
	now := time.Now()
	if len(c.seen) > 0 && now.Sub(c.pruned) > time.Second {
		c.pruned = now
		maps.DeleteFunc(c.seen, func(_ burstKey, s seenBurst) bool { return now.Sub(s.at) >= sameBurstFor })
	}
	k := burstKey{stream: b.GetStreamId(), sequence: b.GetSequence(), slot: b.GetSlot(), source: b.GetSource()}
	if got, ok := c.seen[k]; ok && got.leg != l && now.Sub(got.at) < sameBurstFor {
		return true
	}
	if len(c.legs) > 1 || c.moving {
		c.seen[k] = seenBurst{leg: l, at: now}
	}
	return false
}

// move connects again until a stream is up or moveFor passed, and then
// sends there what the plugin sends. The old stream keeps working until the
// hub ends it.
func (c *Conn) move() {
	deadline := time.Now().Add(c.moveFor)
	var l *leg
	for {
		var err error
		wait := time.Until(deadline)
		if wait <= 0 {
			break
		}
		l, err = c.dial(wait)
		if err == nil || !time.Now().Add(c.moveEvery).Before(deadline) {
			break
		}
		t := time.NewTimer(c.moveEvery)
		select {
		case <-c.done:
			t.Stop()
			return
		case <-t.C:
		}
	}
	if l != nil {
		c.sendMu.Lock()
		if c.tracking {
			_ = l.stream.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Tracking: true}}})
		}
		c.mu.Lock()
		closed := false
		select {
		case <-c.done:
			closed = true
		default:
			c.cur = l
			c.legs = append(c.legs, l)
		}
		c.mu.Unlock()
		c.sendMu.Unlock()
		if closed {
			l.close()
			return
		}
		go c.read(l)
	}
	select {
	case c.events <- event{moved: true}:
	case <-c.done:
	}
}

// Send sends a burst. It is safe to call from several goroutines.
func (c *Conn) Send(b *pluginv1.Burst) error {
	return c.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: b}})
}

// SubscribeTracking asks the hub for the radios whose owners turned
// position tracking on, and for where they are heard, or, with on false,
// stops that.
func (c *Conn) SubscribeTracking(on bool) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	c.tracking = on
	return c.sendLocked(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Tracking: on}}})
}

// SendPosition gives the hub a position that a radio reported to one of
// the plugin's IDs. It is safe to call from several goroutines.
func (c *Conn) SendPosition(p *pluginv1.Position) error {
	return c.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Position{Position: p}})
}

func (c *Conn) send(m *pluginv1.ConnectRequest) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	return c.sendLocked(m)
}

func (c *Conn) sendLocked(m *pluginv1.ConnectRequest) error {
	c.mu.Lock()
	l := c.cur
	c.mu.Unlock()
	if err := l.stream.Send(m); err != nil {
		return fmt.Errorf("could not send to the hub: %w", err)
	}
	return nil
}

// Close ends the connection.
func (c *Conn) Close() error {
	c.sendMu.Lock()
	c.mu.Lock()
	first := false
	c.once.Do(func() {
		first = true
		close(c.done)
	})
	legs := slices.Clone(c.legs)
	c.mu.Unlock()
	if first {
		for _, l := range legs {
			_ = l.stream.CloseSend()
		}
	}
	c.sendMu.Unlock()
	c.stop()
	var err error
	for _, l := range legs {
		l.once.Do(func() {
			l.cancel()
			err = errors.Join(err, l.conn.Close())
		})
	}
	if err != nil {
		return fmt.Errorf("could not close the connection: %w", err)
	}
	return nil
}
