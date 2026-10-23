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

// Package aprstest is a fake APRS-IS server on loopback, so tests never
// reach the real network.
package aprstest

import (
	"bufio"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// Server accepts APRS-IS logins on 127.0.0.1 and records the lines that
// logged-in clients send.
type Server struct {
	// Passcode is the passcode that the server verifies. Any other is
	// answered as unverified.
	Passcode string

	ln     net.Listener
	mu     sync.Mutex
	logins []string
	lines  []string
	conns  []net.Conn
	wg     sync.WaitGroup
}

// NewServer starts a Server that verifies passcode, closed at the end of
// the test.
func NewServer(t *testing.T, passcode string) *Server {
	t.Helper()
	var lc net.ListenConfig
	ln, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("could not listen: %v", err)
	}
	s := &Server{Passcode: passcode, ln: ln}
	s.wg.Add(1)
	go s.accept()
	t.Cleanup(s.Close)
	return s
}

// Addr returns the address of the server, as host:port.
func (s *Server) Addr() string {
	return s.ln.Addr().String()
}

// Close stops the server and closes its connections.
func (s *Server) Close() {
	_ = s.ln.Close()
	s.Drop()
	s.wg.Wait()
}

// Drop closes the connections of the server, as a server restart would.
func (s *Server) Drop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.conns {
		_ = c.Close()
	}
	s.conns = nil
}

// Logins returns the login lines that the server received.
func (s *Server) Logins() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.logins...)
}

// Lines returns the lines that verified clients sent after logging in.
func (s *Server) Lines() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

// WaitLines waits up to wait for n lines and returns the lines it has.
func (s *Server) WaitLines(n int, wait time.Duration) []string {
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if got := s.Lines(); len(got) >= n {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	return s.Lines()
}

func (s *Server) accept() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		s.mu.Lock()
		s.conns = append(s.conns, c)
		s.mu.Unlock()
		s.wg.Add(1)
		go s.serve(c)
	}
}

func (s *Server) serve(c net.Conn) {
	defer s.wg.Done()
	defer func() { _ = c.Close() }()
	if _, err := c.Write([]byte("# aprstest 1.0\r\n")); err != nil {
		return
	}
	r := bufio.NewScanner(c)
	if !r.Scan() {
		return
	}
	login := r.Text()
	s.mu.Lock()
	s.logins = append(s.logins, login)
	s.mu.Unlock()
	fields := strings.Fields(login)
	if len(fields) < 4 || fields[0] != "user" || fields[2] != "pass" {
		return
	}
	verified := fields[3] == s.Passcode
	state := "unverified"
	if verified {
		state = "verified"
	}
	if _, err := c.Write([]byte("# logresp " + fields[1] + " " + state + ", server TEST\r\n")); err != nil {
		return
	}
	for r.Scan() {
		if !verified || strings.HasPrefix(r.Text(), "#") {
			continue
		}
		s.mu.Lock()
		s.lines = append(s.lines, r.Text())
		s.mu.Unlock()
	}
}
