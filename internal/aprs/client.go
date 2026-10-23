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

package aprs

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"strings"
	"sync"
	"time"
)

// ErrNotLoggedIn is returned by Send while the client has no verified
// login.
var ErrNotLoggedIn = errors.New("not logged in to APRS-IS")

const (
	dialTimeout  = 10 * time.Second
	writeTimeout = 10 * time.Second
	// readTimeout is how long the client waits for a line from the server,
	// which sends a comment at least every 20 seconds.
	readTimeout = 2 * time.Minute
	minBackoff  = 5 * time.Second
	maxBackoff  = 5 * time.Minute
)

// Client keeps one login to APRS-IS and sends lines on it.
type Client struct {
	cfg     Config
	version string
	backoff time.Duration

	mu       sync.Mutex
	conn     net.Conn
	verified bool

	cancel context.CancelFunc
	done   chan struct{}
}

// NewClient returns a Client that logs in with cfg as DMRHub version.
func NewClient(cfg Config, version string) *Client {
	return &Client{cfg: cfg, version: strings.Join(strings.Fields(version), "-"), backoff: minBackoff}
}

// Config returns the login of the client.
func (c *Client) Config() Config {
	return c.cfg
}

// Start connects and logs in, and does again each time the connection
// ends, until Stop.
func (c *Client) Start(ctx context.Context) {
	ctx, c.cancel = context.WithCancel(ctx)
	c.done = make(chan struct{})
	go c.run(ctx)
}

// Stop closes the connection and waits for the client to end.
func (c *Client) Stop() {
	if c.cancel == nil {
		return
	}
	c.cancel()
	c.mu.Lock()
	if c.conn != nil {
		_ = c.conn.Close()
	}
	c.mu.Unlock()
	<-c.done
}

// LoggedIn reports whether the server verified the login.
func (c *Client) LoggedIn() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.verified
}

// Send writes line to APRS-IS. It returns ErrNotLoggedIn while the login
// is not verified, since APRS-IS drops what an unverified client sends.
func (c *Client) Send(line string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil || !c.verified {
		return ErrNotLoggedIn
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("could not send to APRS-IS: %w", err)
	}
	if _, err := c.conn.Write([]byte(line + "\r\n")); err != nil {
		_ = c.conn.Close()
		return fmt.Errorf("could not send to APRS-IS: %w", err)
	}
	return nil
}

func (c *Client) run(ctx context.Context) {
	defer close(c.done)
	backoff := c.backoff
	for ctx.Err() == nil {
		start := time.Now()
		err := c.session(ctx)
		if ctx.Err() != nil {
			return
		}
		slog.Warn("APRS-IS: connection ended", "server", c.cfg.Server, "error", err)
		if time.Since(start) > maxBackoff {
			backoff = c.backoff
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(backoff):
		}
		backoff = min(backoff*2, maxBackoff)
	}
}

func (c *Client) session(ctx context.Context) error {
	d := net.Dialer{Timeout: dialTimeout}
	conn, err := d.DialContext(ctx, "tcp", c.cfg.Server)
	if err != nil {
		return fmt.Errorf("could not connect: %w", err)
	}
	c.mu.Lock()
	c.conn, c.verified = conn, false
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		c.conn, c.verified = nil, false
		c.mu.Unlock()
		_ = conn.Close()
	}()
	login := fmt.Sprintf("user %s pass %s vers DMRHub %s\r\n", c.cfg.Login, c.cfg.Passcode, c.version)
	if err := conn.SetWriteDeadline(time.Now().Add(writeTimeout)); err != nil {
		return fmt.Errorf("could not log in: %w", err)
	}
	if _, err := conn.Write([]byte(login)); err != nil {
		return fmt.Errorf("could not log in: %w", err)
	}
	r := bufio.NewScanner(conn)
	for {
		if err := conn.SetReadDeadline(time.Now().Add(readTimeout)); err != nil {
			return fmt.Errorf("could not read: %w", err)
		}
		if !r.Scan() {
			if err := r.Err(); err != nil {
				return fmt.Errorf("could not read: %w", err)
			}
			return errors.New("the server closed the connection")
		}
		c.serverLine(r.Text())
	}
}

func (c *Client) serverLine(line string) {
	fields := strings.Fields(strings.TrimPrefix(line, "#"))
	if len(fields) < 3 || fields[0] != "logresp" || !strings.EqualFold(fields[1], c.cfg.Login) {
		return
	}
	verified := strings.TrimSuffix(fields[2], ",") == "verified"
	c.mu.Lock()
	c.verified = verified
	c.mu.Unlock()
	if verified {
		slog.Info("APRS-IS: logged in", "login", c.cfg.Login, "server", c.cfg.Server)
	} else {
		slog.Error("APRS-IS: the server did not verify the login; check the passcode", "login", c.cfg.Login, "server", c.cfg.Server)
	}
}
