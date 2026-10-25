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

package testutils

import (
	"errors"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
)

// UDPFront is a UDP load balancer in front of the replicas of a test, as
// the UDP proxy of Envoy Gateway is in front of the pods. Each client gets
// a session with a socket of its own towards one backend, so a replica
// sees the address of that socket and not the client's. When the backend
// of a session drains, as Envoy Gateway marks a pod that is terminating,
// the next packet of the client opens a new session, from a new socket, to
// a backend that does not drain. When a backend is removed, its sessions
// end, and the next packet of each client opens a new one.
type UDPFront struct {
	conn *net.UDPConn

	mu       sync.Mutex
	backends []*frontBackend
	sessions map[string]*frontSession
	moves    int
	wg       sync.WaitGroup
}

type frontBackend struct {
	addr     *net.UDPAddr
	draining bool
	sessions int
}

type frontSession struct {
	client  *net.UDPAddr
	up      *net.UDPConn
	backend *frontBackend
}

// NewUDPFront starts a UDPFront on a port of the loopback with no backend.
// The test stops it at its end.
func NewUDPFront(t *testing.T) *UDPFront {
	t.Helper()
	conn, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	f := &UDPFront{conn: conn, sessions: map[string]*frontSession{}}
	f.wg.Add(1)
	go f.serve()
	t.Cleanup(f.close)
	return f
}

// Addr returns the address that the clients send to.
func (f *UDPFront) Addr() string {
	return f.conn.LocalAddr().String()
}

// Add adds the backend at addr.
func (f *UDPFront) Add(t *testing.T, addr string) {
	t.Helper()
	a, err := net.ResolveUDPAddr("udp", addr)
	require.NoError(t, err)
	f.mu.Lock()
	defer f.mu.Unlock()
	f.backends = append(f.backends, &frontBackend{addr: a})
}

// Drain marks the backend at addr as draining: it gets no new session, and
// its sessions move at the next packet of their clients.
func (f *UDPFront) Drain(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if b := f.find(addr); b != nil {
		b.draining = true
	}
}

// Remove removes the backend at addr and ends its sessions.
func (f *UDPFront) Remove(addr string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.find(addr)
	if b == nil {
		return
	}
	for key, s := range f.sessions {
		if s.backend == b {
			_ = s.up.Close()
			delete(f.sessions, key)
		}
	}
	for i, x := range f.backends {
		if x == b {
			f.backends = append(f.backends[:i], f.backends[i+1:]...)
			break
		}
	}
}

// Moves returns how many sessions moved from a draining backend to another.
func (f *UDPFront) Moves() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.moves
}

func (f *UDPFront) find(addr string) *frontBackend {
	for _, b := range f.backends {
		if b.addr.String() == addr {
			return b
		}
	}
	return nil
}

func (f *UDPFront) pick() *frontBackend {
	var best *frontBackend
	for _, b := range f.backends {
		if !b.draining && (best == nil || b.sessions < best.sessions) {
			best = b
		}
	}
	return best
}

func (f *UDPFront) serve() {
	defer f.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, client, err := f.conn.ReadFromUDP(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		if s := f.sessionFor(client); s != nil {
			_, _ = s.up.Write(buf[:n])
		}
	}
}

func (f *UDPFront) sessionFor(client *net.UDPAddr) *frontSession {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sessions[client.String()]
	if ok && !s.backend.draining {
		return s
	}
	b := f.pick()
	if b == nil {
		return s
	}
	if ok {
		f.moves++
		s.backend.sessions--
		_ = s.up.Close()
		delete(f.sessions, client.String())
	}
	up, err := net.DialUDP("udp", nil, b.addr)
	if err != nil {
		return nil
	}
	s = &frontSession{client: client, up: up, backend: b}
	b.sessions++
	f.sessions[client.String()] = s
	f.wg.Add(1)
	go f.back(s)
	return s
}

func (f *UDPFront) back(s *frontSession) {
	defer f.wg.Done()
	buf := make([]byte, 65535)
	for {
		n, err := s.up.Read(buf)
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			continue
		}
		_, _ = f.conn.WriteToUDP(buf[:n], s.client)
	}
}

func (f *UDPFront) close() {
	_ = f.conn.Close()
	f.mu.Lock()
	for _, s := range f.sessions {
		_ = s.up.Close()
	}
	f.mu.Unlock()
	f.wg.Wait()
}
