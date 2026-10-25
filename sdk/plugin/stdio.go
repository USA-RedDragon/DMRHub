// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sync"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

const (
	// opDial names the dial in the errors of a connection.
	opDial = "dial"
	// netTCP is the network of the connections.
	netTCP = "tcp"
)

// errHubClosed is returned when the hub of a plugin inside DMRHub closed
// the plugin's stdin.
var errHubClosed = errors.New("the hub closed the connection")

// stdio is the connection of a plugin inside DMRHub to its hub, over the
// plugin's stdin and stdout. Messages from the hub wait in a queue until
// the plugin takes them, so that the answers of the network always get
// through.
type stdio struct {
	w   io.Writer
	wmu sync.Mutex

	mu    sync.Mutex
	more  *sync.Cond
	queue []*pluginv1.ConnectResponse
	err   error
	done  chan struct{}
	conns map[uint32]*tunnel
	next  uint32
}

func newStdio(r io.Reader, w io.Writer) *stdio {
	s := &stdio{w: w, done: make(chan struct{}), conns: map[uint32]*tunnel{}}
	s.more = sync.NewCond(&s.mu)
	go s.read(r)
	return s
}

func (s *stdio) read(r io.Reader) {
	br := bufio.NewReaderSize(r, MaxPluginMessage)
	for {
		m := &pluginv1.ConnectResponse{}
		if err := ReadFrame(br, m, MaxHubMessage); err != nil {
			s.mu.Lock()
			s.err = err
			close(s.done)
			s.more.Broadcast()
			s.mu.Unlock()
			return
		}
		if n := m.GetNet(); n != nil {
			s.answer(n)
			continue
		}
		s.mu.Lock()
		s.queue = append(s.queue, m)
		s.more.Signal()
		s.mu.Unlock()
	}
}

func (s *stdio) recv() (*pluginv1.ConnectResponse, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.queue) == 0 && s.err == nil {
		s.more.Wait()
	}
	if len(s.queue) == 0 {
		return nil, fmt.Errorf("%w: %w", errHubClosed, s.err)
	}
	m := s.queue[0]
	s.queue[0] = nil
	s.queue = s.queue[1:]
	return m, nil
}

func (s *stdio) send(m *pluginv1.ConnectRequest) error {
	s.wmu.Lock()
	defer s.wmu.Unlock()
	if err := WriteFrame(s.w, m); err != nil {
		return fmt.Errorf("could not send to the hub: %w", err)
	}
	return nil
}

func (s *stdio) answer(r *pluginv1.NetResponse) {
	s.mu.Lock()
	t := s.conns[r.GetConn()]
	s.mu.Unlock()
	if t == nil {
		return
	}
	var ch chan *pluginv1.NetResponse
	switch r.GetOp() {
	case pluginv1.NetOp_NET_OP_OPEN:
		ch = t.opened
	case pluginv1.NetOp_NET_OP_READ:
		ch = t.reads
	case pluginv1.NetOp_NET_OP_WRITE:
		ch = t.writes
	case pluginv1.NetOp_NET_OP_CLOSE, pluginv1.NetOp_NET_OP_UNSPECIFIED:
		return
	}
	select {
	case ch <- r:
	default:
	}
}

func (s *stdio) forget(id uint32) {
	s.mu.Lock()
	delete(s.conns, id)
	s.mu.Unlock()
}

func netRequest(id uint32, op any) *pluginv1.ConnectRequest {
	r := &pluginv1.NetRequest{Conn: id}
	switch o := op.(type) {
	case string:
		r.Op = &pluginv1.NetRequest_Open{Open: o}
	case uint32:
		r.Op = &pluginv1.NetRequest_Read{Read: o}
	case []byte:
		r.Op = &pluginv1.NetRequest_Write{Write: o}
	default:
		r.Op = &pluginv1.NetRequest_Close{Close: &pluginv1.NetClose{}}
	}
	return &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Net{Net: r}}
}

// dial opens a TCP connection to address through the hub.
func (s *stdio) dial(ctx context.Context, address string) (net.Conn, error) {
	s.mu.Lock()
	if s.err != nil {
		s.mu.Unlock()
		return nil, &net.OpError{Op: opDial, Net: netTCP, Addr: tunnelAddr(address), Err: errHubClosed}
	}
	s.next++
	t := &tunnel{
		s: s, id: s.next, address: address,
		opened: make(chan *pluginv1.NetResponse, 1), reads: make(chan *pluginv1.NetResponse, 1), writes: make(chan *pluginv1.NetResponse, 1),
		closed: make(chan struct{}), rkick: make(chan struct{}, 1), wkick: make(chan struct{}, 1),
	}
	s.conns[t.id] = t
	s.mu.Unlock()
	if err := s.send(netRequest(t.id, address)); err != nil {
		s.forget(t.id)
		return nil, err
	}
	select {
	case r := <-t.opened:
		if r.GetError() != "" {
			s.forget(t.id)
			return nil, &net.OpError{Op: opDial, Net: netTCP, Addr: tunnelAddr(address), Err: errors.New(r.GetError())} //nolint:err113 // the hub's reason
		}
		return t, nil
	case <-ctx.Done():
		_ = t.Close()
		return nil, &net.OpError{Op: opDial, Net: netTCP, Addr: tunnelAddr(address), Err: ctx.Err()}
	case <-s.done:
		s.forget(t.id)
		return nil, &net.OpError{Op: opDial, Net: netTCP, Addr: tunnelAddr(address), Err: errHubClosed}
	}
}

// tunnelAddr is the address of a host that a plugin inside DMRHub reaches
// through the hub.
type tunnelAddr string

// Network returns "tcp".
func (tunnelAddr) Network() string { return netTCP }

func (a tunnelAddr) String() string { return string(a) }

// tunnel is a TCP connection of a plugin inside DMRHub, which the hub opens
// for it.
type tunnel struct {
	s       *stdio
	id      uint32
	address string

	opened, reads, writes chan *pluginv1.NetResponse
	closed                chan struct{}
	closeOnce             sync.Once

	rmu     sync.Mutex
	pending bool
	left    []byte
	eof     bool

	wmu sync.Mutex

	dmu          sync.Mutex
	rdl, wdl     time.Time
	rkick, wkick chan struct{}
}

func (t *tunnel) opError(op string, err error) error {
	return &net.OpError{Op: op, Net: netTCP, Addr: tunnelAddr(t.address), Err: err}
}

func (t *tunnel) deadline(read bool) time.Time {
	t.dmu.Lock()
	defer t.dmu.Unlock()
	if read {
		return t.rdl
	}
	return t.wdl
}

// wait waits for the answer on ch, until the deadline of its direction,
// which can change while it waits.
func (t *tunnel) wait(ch chan *pluginv1.NetResponse, read bool) (*pluginv1.NetResponse, error) {
	kick := t.wkick
	if read {
		kick = t.rkick
	}
	for {
		var timeout <-chan time.Time
		var timer *time.Timer
		if dl := t.deadline(read); !dl.IsZero() {
			d := time.Until(dl)
			if d <= 0 {
				return nil, os.ErrDeadlineExceeded
			}
			timer = time.NewTimer(d)
			timeout = timer.C
		}
		select {
		case r := <-ch:
			stop(timer)
			return r, nil
		case <-timeout:
			return nil, os.ErrDeadlineExceeded
		case <-kick:
			stop(timer)
		case <-t.closed:
			stop(timer)
			return nil, net.ErrClosed
		case <-t.s.done:
			stop(timer)
			return nil, errHubClosed
		}
	}
}

func stop(t *time.Timer) {
	if t != nil {
		t.Stop()
	}
}

// Read reads what the host sent.
func (t *tunnel) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	t.rmu.Lock()
	defer t.rmu.Unlock()
	if len(t.left) > 0 {
		n := copy(p, t.left)
		t.left = t.left[n:]
		return n, nil
	}
	if t.eof {
		return 0, io.EOF
	}
	if !t.pending {
		if err := t.s.send(netRequest(t.id, uint32(min(len(p), MaxNetChunk)))); err != nil { //nolint:gosec // at most MaxNetChunk
			return 0, t.opError("read", err)
		}
		t.pending = true
	}
	r, err := t.wait(t.reads, true)
	if err != nil {
		return 0, t.opError("read", err)
	}
	t.pending = false
	if r.GetError() != "" {
		return 0, t.opError("read", errors.New(r.GetError())) //nolint:err113 // the hub's reason
	}
	n := copy(p, r.GetData())
	t.left = r.GetData()[n:]
	if r.GetEof() {
		t.eof = true
		if n == 0 && len(t.left) == 0 {
			return 0, io.EOF
		}
	}
	return n, nil
}

// Write sends p to the host. A write that misses its deadline closes the
// connection, because the hub may still send part of it.
func (t *tunnel) Write(p []byte) (int, error) {
	t.wmu.Lock()
	defer t.wmu.Unlock()
	sent := 0
	for sent < len(p) {
		chunk := p[sent:min(len(p), sent+MaxNetChunk)]
		if err := t.s.send(netRequest(t.id, chunk)); err != nil {
			return sent, t.opError("write", err)
		}
		r, err := t.wait(t.writes, false)
		if err != nil {
			if errors.Is(err, os.ErrDeadlineExceeded) {
				_ = t.Close()
			}
			return sent, t.opError("write", err)
		}
		sent += int(r.GetWritten())
		if r.GetError() != "" {
			return sent, t.opError("write", errors.New(r.GetError())) //nolint:err113 // the hub's reason
		}
	}
	return sent, nil
}

// Close closes the connection.
func (t *tunnel) Close() error {
	first := false
	t.closeOnce.Do(func() {
		first = true
		close(t.closed)
	})
	if !first {
		return t.opError("close", net.ErrClosed)
	}
	t.s.forget(t.id)
	select {
	case <-t.s.done:
		return nil
	default:
	}
	return t.s.send(netRequest(t.id, nil))
}

// LocalAddr returns the address of the host, as the plugin has no address
// of its own.
func (t *tunnel) LocalAddr() net.Addr { return tunnelAddr(t.address) }

// RemoteAddr returns the address of the host.
func (t *tunnel) RemoteAddr() net.Addr { return tunnelAddr(t.address) }

// SetDeadline sets the read and write deadlines.
func (t *tunnel) SetDeadline(d time.Time) error {
	_ = t.SetReadDeadline(d)
	return t.SetWriteDeadline(d)
}

// SetReadDeadline sets the deadline of reads, also of one that waits.
func (t *tunnel) SetReadDeadline(d time.Time) error {
	t.dmu.Lock()
	t.rdl = d
	t.dmu.Unlock()
	select {
	case t.rkick <- struct{}{}:
	default:
	}
	return nil
}

// SetWriteDeadline sets the deadline of writes, also of one that waits.
func (t *tunnel) SetWriteDeadline(d time.Time) error {
	t.dmu.Lock()
	t.wdl = d
	t.dmu.Unlock()
	select {
	case t.wkick <- struct{}{}:
	default:
	}
	return nil
}

// handshake tells the hub who the plugin is with hello, and returns the
// hub's Welcome.
func handshake(s *stdio, hello *pluginv1.Hello) (*pluginv1.Welcome, error) {
	if err := s.send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Hello{Hello: hello}}); err != nil {
		return nil, err
	}
	m, err := s.recv()
	if err != nil {
		return nil, fmt.Errorf("could not connect to the hub: %w", err)
	}
	if m.GetWelcome() == nil {
		return nil, ErrNoWelcome
	}
	return m.GetWelcome(), nil
}
