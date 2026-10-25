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

package wasm

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// The reasons a plugin gets for a connection that it may not open or use.
const (
	reasonNotAllowed  = "an admin did not allow this host on the plugin's page"
	reasonAddress     = "this host has an address that plugins may not reach"
	reasonTooMany     = "too many connections"
	reasonNoSuchConn  = "no such connection"
	reasonInUse       = "one read and one write at a time"
	reasonAlreadyOpen = "the connection is already open"
)

var errAddress = errors.New(reasonAddress)

// netHost opens and serves the TCP connections of a plugin inside DMRHub,
// to the hosts that an admin allowed.
type netHost struct {
	i     *Instance
	hosts Hosts

	mu    sync.Mutex
	conns map[uint32]*hostConn
}

type hostConn struct {
	address string
	conn    net.Conn
	closed  bool

	mu               sync.Mutex
	reading, writing bool
}

func newNetHost(i *Instance, hosts Hosts) *netHost {
	return &netHost{i: i, hosts: hosts, conns: map[uint32]*hostConn{}}
}

func (h *netHost) allowed(address string) bool {
	return h.hosts != nil && h.hosts(address)
}

func (h *netHost) answer(r *pluginv1.NetResponse) {
	_ = h.i.write(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Net{Net: r}})
}

func (h *netHost) fail(id uint32, op pluginv1.NetOp, reason string) {
	h.answer(&pluginv1.NetResponse{Conn: id, Op: op, Error: reason})
}

// handle serves a request of the plugin. It answers in the background, so
// that a slow host never holds up the plugin's other messages.
func (h *netHost) handle(r *pluginv1.NetRequest) {
	id := r.GetConn()
	switch op := r.GetOp().(type) {
	case *pluginv1.NetRequest_Open:
		h.open(id, op.Open)
	case *pluginv1.NetRequest_Read:
		h.read(id, op.Read)
	case *pluginv1.NetRequest_Write:
		h.write(id, op.Write)
	case *pluginv1.NetRequest_Close:
		h.close(id)
	}
}

func (h *netHost) open(id uint32, address string) {
	if !h.allowed(address) {
		h.fail(id, pluginv1.NetOp_NET_OP_OPEN, reasonNotAllowed)
		return
	}
	h.mu.Lock()
	if _, open := h.conns[id]; open {
		h.mu.Unlock()
		h.fail(id, pluginv1.NetOp_NET_OP_OPEN, reasonAlreadyOpen)
		return
	}
	if len(h.conns) >= h.i.limits.MaxConns {
		h.mu.Unlock()
		h.fail(id, pluginv1.NetOp_NET_OP_OPEN, reasonTooMany)
		return
	}
	c := &hostConn{address: address}
	h.conns[id] = c
	h.mu.Unlock()
	go func() {
		conn, err := h.dial(address)
		h.mu.Lock()
		if c.closed || h.conns[id] != c {
			h.mu.Unlock()
			if conn != nil {
				_ = conn.Close()
			}
			return
		}
		if err != nil {
			delete(h.conns, id)
			h.mu.Unlock()
			reason := err.Error()
			if errors.Is(err, errAddress) {
				reason = reasonAddress
			}
			h.fail(id, pluginv1.NetOp_NET_OP_OPEN, reason)
			return
		}
		c.conn = conn
		h.mu.Unlock()
		h.answer(&pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_OPEN})
	}()
}

func (h *netHost) dial(address string) (net.Conn, error) {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err //nolint:wrapcheck // the reason the plugin gets
	}
	literal := net.ParseIP(host) != nil
	allow := h.i.limits.AllowAddress
	d := net.Dialer{
		Timeout: h.i.limits.DialTimeout,
		Control: func(_, addr string, _ syscall.RawConn) error {
			ip, _, err := net.SplitHostPort(addr)
			if err != nil {
				return errAddress
			}
			if parsed := net.ParseIP(ip); parsed == nil || allow == nil || !allow(parsed, literal) {
				return errAddress
			}
			return nil
		},
	}
	ctx, cancel := context.WithTimeout(h.i.ctx, h.i.limits.DialTimeout)
	defer cancel()
	return d.DialContext(ctx, "tcp", address) //nolint:wrapcheck // the reason the plugin gets
}

// use returns the open connection id for a read or a write, and false
// with the reason when there is none or it is busy.
func (h *netHost) use(id uint32, write bool) (*hostConn, string) {
	h.mu.Lock()
	c := h.conns[id]
	open := c != nil && c.conn != nil
	h.mu.Unlock()
	if !open {
		return nil, reasonNoSuchConn
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	busy := &c.reading
	if write {
		busy = &c.writing
	}
	if *busy {
		return nil, reasonInUse
	}
	*busy = true
	return c, ""
}

func (c *hostConn) done(write bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if write {
		c.writing = false
	} else {
		c.reading = false
	}
}

func (h *netHost) read(id uint32, n uint32) {
	c, reason := h.use(id, false)
	if c == nil {
		h.fail(id, pluginv1.NetOp_NET_OP_READ, reason)
		return
	}
	go func() {
		buf := make([]byte, max(1, min(int(n), plugin.MaxNetChunk)))
		_ = c.conn.SetReadDeadline(time.Now().Add(h.i.limits.IdleTimeout))
		got, err := c.conn.Read(buf)
		c.done(false)
		r := &pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_READ, Data: buf[:got]}
		switch {
		case errors.Is(err, io.EOF):
			r.Eof = true
		case err != nil:
			r.Error = err.Error()
		}
		h.answer(r)
	}()
}

func (h *netHost) write(id uint32, data []byte) {
	c, reason := h.use(id, true)
	if c == nil {
		h.fail(id, pluginv1.NetOp_NET_OP_WRITE, reason)
		return
	}
	go func() {
		_ = c.conn.SetWriteDeadline(time.Now().Add(h.i.limits.IdleTimeout))
		n, err := c.conn.Write(data)
		c.done(true)
		r := &pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_WRITE, Written: uint32(n)} //nolint:gosec // at most MaxPluginMessage
		if err != nil {
			r.Error = err.Error()
		}
		h.answer(r)
	}()
}

func (h *netHost) close(id uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if c := h.conns[id]; c != nil {
		delete(h.conns, id)
		c.shut()
	}
}

// shut closes c. Call it with the lock of its netHost.
func (c *hostConn) shut() {
	c.closed = true
	if c.conn != nil {
		_ = c.conn.Close()
	}
}

// refresh closes the connections to the hosts that are no longer allowed.
func (h *netHost) refresh() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, c := range h.conns {
		if !h.allowed(c.address) {
			delete(h.conns, id)
			c.shut()
		}
	}
}

func (h *netHost) closeAll() {
	h.mu.Lock()
	defer h.mu.Unlock()
	for id, c := range h.conns {
		delete(h.conns, id)
		c.shut()
	}
}
