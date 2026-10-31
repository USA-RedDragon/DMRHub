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
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// PipeListener is a net.Listener whose connections are net.Pipe pairs in
// memory. A server on it runs in a synctest bubble, which cannot hold a
// real socket.
type PipeListener struct {
	conns  chan net.Conn
	done   chan struct{}
	once   sync.Once
	client *http.Client
}

type pipeAddr struct{}

func (pipeAddr) Network() string { return "pipe" }
func (pipeAddr) String() string  { return "pipe" }

// NewPipeListener returns a PipeListener that tb closes, with the idle
// connections of its Client, when the test ends.
func NewPipeListener(tb testing.TB) *PipeListener {
	tb.Helper()
	l := &PipeListener{conns: make(chan net.Conn), done: make(chan struct{})}
	transport := &http.Transport{DialContext: l.DialContext}
	l.client = &http.Client{Transport: transport}
	tb.Cleanup(func() {
		_ = l.Close()
		transport.CloseIdleConnections()
	})
	return l
}

// Accept waits for the next connection that DialContext makes.
func (l *PipeListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

// Close stops Accept and DialContext. It is safe to call more than once.
func (l *PipeListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

// Addr returns the address "pipe".
func (l *PipeListener) Addr() net.Addr {
	return pipeAddr{}
}

// DialContext connects to the listener, whatever network and address it
// is given.
func (l *PipeListener) DialContext(ctx context.Context, _, _ string) (net.Conn, error) {
	client, server := net.Pipe()
	select {
	case l.conns <- server:
		return client, nil
	case <-ctx.Done():
	case <-l.done:
	}
	_ = client.Close()
	_ = server.Close()
	if err := ctx.Err(); err != nil {
		return nil, err //nolint:wrapcheck // the error of the caller's context
	}
	return nil, net.ErrClosed
}

// Client returns an HTTP client whose connections all go to the listener.
func (l *PipeListener) Client() *http.Client {
	return l.client
}

// StartPipeServer starts an httptest.Server of h on a PipeListener, which
// tb closes when the test ends, and returns it with the client that
// reaches it.
func StartPipeServer(tb testing.TB, h http.Handler) (*httptest.Server, *http.Client) {
	tb.Helper()
	l := NewPipeListener(tb)
	s := &httptest.Server{Listener: l, Config: &http.Server{Handler: h, ReadHeaderTimeout: time.Minute}}
	s.Start()
	tb.Cleanup(s.Close)
	return s, l.Client()
}
