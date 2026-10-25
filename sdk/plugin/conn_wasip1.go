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
	"net"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// Conn is a plugin's connection to its hub. A plugin inside DMRHub talks to
// the hub on its stdin and stdout.
type Conn struct {
	s       *stdio
	welcome *pluginv1.Welcome
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
// stops that.
func (c *Conn) SubscribeTracking(on bool) error {
	return c.s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Tracking: on}}})
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
