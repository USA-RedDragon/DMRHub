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

// Package plugin connects a DMRHub plugin to its hub. A plugin answers the
// calls to the DMR IDs an admin gave it, and sends calls only from those IDs.
//
//	conn, err := plugin.Dial(ctx, plugin.Options{Address: "hub.example.org:3006", Token: token, Fingerprint: fp})
//	for {
//		burst, err := conn.Recv()
//		...
//		err = conn.Send(reply)
//	}
package plugin

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
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

// Conn is a plugin's connection to its hub.
type Conn struct {
	conn    *grpc.ClientConn
	stream  pluginv1.PluginService_ConnectClient
	welcome *pluginv1.Welcome

	sendMu sync.Mutex
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

// Dial connects to the hub and waits for its Welcome.
func Dial(ctx context.Context, o Options) (*Conn, error) {
	conn, err := grpc.NewClient(o.Address,
		grpc.WithTransportCredentials(credentials.NewTLS(tlsConfig(o.Fingerprint))),
		grpc.WithKeepaliveParams(keepalive.ClientParameters{Time: pingEvery, Timeout: pingEvery, PermitWithoutStream: true}),
	)
	if err != nil {
		return nil, fmt.Errorf("could not reach the hub: %w", err)
	}
	ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+o.Token)
	stream, err := pluginv1.NewPluginServiceClient(conn).Connect(ctx)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("could not connect to the hub: %w", err)
	}
	first, err := stream.Recv()
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("could not connect to the hub: %w", err)
	}
	if first.GetWelcome() == nil {
		_ = conn.Close()
		return nil, ErrNoWelcome
	}
	return &Conn{conn: conn, stream: stream, welcome: first.GetWelcome()}, nil
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
// plugin's IDs, a refusal of one of its bursts, or its new IDs.
func (c *Conn) Recv() (*pluginv1.ConnectResponse, error) {
	m, err := c.stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("the hub closed the connection: %w", err)
	}
	return m, nil
}

// Send sends a burst. It is safe to call from several goroutines.
func (c *Conn) Send(b *pluginv1.Burst) error {
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	if err := c.stream.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: b}}); err != nil {
		return fmt.Errorf("could not send a burst: %w", err)
	}
	return nil
}

// Close ends the connection.
func (c *Conn) Close() error {
	c.sendMu.Lock()
	_ = c.stream.CloseSend()
	c.sendMu.Unlock()
	if err := c.conn.Close(); err != nil {
		return fmt.Errorf("could not close the connection: %w", err)
	}
	return nil
}
