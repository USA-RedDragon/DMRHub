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
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// UDPFront is a UDP load balancer in front of the replicas of a test, as
// the UDP proxy of Envoy Gateway is in front of the pods. Each client gets
// a session with a socket of its own towards one backend, so a replica
// sees the address of that socket and not the client's. When the backend
// of a session drains, as Envoy Gateway marks a pod that is terminating,
// the next packet of the client opens a new session, from a new socket, to
// a backend that does not drain. When a backend is removed, its sessions
// end, and the next packet of each client opens a new one. Once a session
// ends, nothing more of it reaches the client.
type UDPFront struct {
	conn *net.UDPConn

	mu       sync.Mutex
	backends []*frontBackend
	sessions map[string]*frontSession
	moves    int
	ended    map[string][]time.Time
	// holdUntil drops the packets of clients with no session until then.
	holdUntil time.Time
	wg        sync.WaitGroup
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
	f := &UDPFront{conn: conn, sessions: map[string]*frontSession{}, ended: map[string][]time.Time{}}
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

// Cut marks the backend at addr as draining and ends its sessions at
// once, as Envoy Gateway does with the sessions of a pod that leaves its
// endpoints, and drops the packets of their clients for hold before it
// opens them new sessions. What the backend sends meanwhile is lost.
func (f *UDPFront) Cut(addr string, hold time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	b := f.find(addr)
	if b == nil {
		return
	}
	b.draining = true
	for key, s := range f.sessions {
		if s.backend == b {
			f.end(key, s)
			b.sessions--
			f.moves++
		}
	}
	f.holdUntil = time.Now().Add(hold)
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
			f.end(key, s)
		}
	}
	for i, x := range f.backends {
		if x == b {
			f.backends = append(f.backends[:i], f.backends[i+1:]...)
			break
		}
	}
}

// end ends the session s of the client key. The caller holds f.mu.
func (f *UDPFront) end(key string, s *frontSession) {
	_ = s.up.Close()
	delete(f.sessions, key)
	f.ended[key] = append(f.ended[key], time.Now())
}

// Ended returns when the sessions of the client at addr ended, as at a
// move, oldest first.
func (f *UDPFront) Ended(addr string) []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Time(nil), f.ended[addr]...)
}

// Ends returns when any session ended, oldest first.
func (f *UDPFront) Ends() []time.Time {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []time.Time
	for _, ended := range f.ended {
		out = append(out, ended...)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return out
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
	if !ok && time.Now().Before(f.holdUntil) {
		return nil
	}
	b := f.pick()
	if b == nil {
		return s
	}
	if ok {
		f.moves++
		s.backend.sessions--
		f.end(client.String(), s)
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
		f.mu.Lock()
		if f.sessions[s.client.String()] == s {
			_, _ = f.conn.WriteToUDP(buf[:n], s.client)
		}
		f.mu.Unlock()
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
