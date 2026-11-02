// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

//go:build wasip1

package plugin

import (
	"context"
	"fmt"
	"net"
	"sync"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// Conn is a plugin's connection to its hub. A plugin inside DMRHub talks to
// the hub on its stdin and stdout.
type Conn struct {
	s       *stdio
	welcome *pluginv1.Welcome

	mu  sync.Mutex
	sub *pluginv1.Subscribe
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
// subscribed to.
func (c *Conn) Recv() (*pluginv1.ConnectResponse, error) {
	return c.s.recv()
}

// Send sends a burst. It is safe to call from several goroutines.
func (c *Conn) Send(b *pluginv1.Burst) error {
	return c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: b}})
}

// SubscribeTracking asks the hub for the radios whose owners turned
// position tracking on, and for where they are heard, or, with on false,
// stops that. It keeps the rest of the last Subscribe.
func (c *Conn) SubscribeTracking(on bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	sub := &pluginv1.Subscribe{}
	if c.sub != nil {
		sub = cloneSubscribe(c.sub)
	}
	sub.Tracking = on
	c.sub = sub
	return c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: sub}})
}

// Subscribe asks the hub for what sub names besides the calls to the
// plugin's IDs, in place of the last Subscribe. It returns ErrUnsupported
// when the hub does not serve a part of sub; Has tells beforehand.
func (c *Conn) Subscribe(sub *pluginv1.Subscribe) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, f := range subscribeFeatures(sub) {
		if !c.Has(f) {
			return fmt.Errorf("%w: %s", ErrUnsupported, f)
		}
	}
	c.sub = cloneSubscribe(sub)
	return c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: c.sub}})
}

// Has reports whether the hub serves f.
func (c *Conn) Has(f pluginv1.Feature) bool {
	return hasFeature(c.welcome, f)
}

// Request sends r with a new id and returns the hub's answer. It returns
// an *AnswerError when the hub answered with an error, ErrUnsupported when
// the hub does not serve r, and ErrLost when the hub closed the
// connection before the answer. It is safe to call from several
// goroutines, also while another goroutine waits in Recv.
func (c *Conn) Request(ctx context.Context, r *pluginv1.Request) (*pluginv1.Answer, error) {
	if f := featureOf(r); !c.Has(f) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupported, f)
	}
	id, ch, ok := c.s.calls.add(&c.s.on)
	if !ok {
		return nil, ErrLost
	}
	r = cloneRequest(r)
	r.Id = id
	if err := c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Request{Request: r}}); err != nil {
		c.s.calls.drop(id)
		return nil, err
	}
	return c.s.calls.await(ctx, id, ch)
}

// SendPosition gives the hub a position that a radio reported to one of
// the plugin's IDs. It is safe to call from several goroutines.
func (c *Conn) SendPosition(p *pluginv1.Position) error {
	return c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Position{Position: p}})
}

// Dial connects to address, as host:port, over TCP. A plugin inside
// DMRHub reaches only the hosts that an admin allowed on its page, through
// the hub.
func (c *Conn) Dial(ctx context.Context, address string) (net.Conn, error) {
	return c.s.dial(ctx, address)
}

// Close does nothing for a plugin inside DMRHub, whose connection ends with
// the plugin.
func (c *Conn) Close() error {
	return nil
}
