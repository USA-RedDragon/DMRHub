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
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"testing"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// fakeHost is the hub's side of the stdio of a plugin inside DMRHub.
type fakeHost struct {
	t   *testing.T
	in  io.Reader
	out io.WriteCloser
	s   *stdio
}

func newFakeHost(t *testing.T) *fakeHost {
	t.Helper()
	guestIn, hostOut := io.Pipe()
	hostIn, guestOut := io.Pipe()
	h := &fakeHost{t: t, in: hostIn, out: hostOut, s: newStdio(guestIn, guestOut)}
	t.Cleanup(func() {
		_ = hostOut.Close()
		_ = guestOut.Close()
	})
	return h
}

func (h *fakeHost) read() *pluginv1.ConnectRequest {
	h.t.Helper()
	var m pluginv1.ConnectRequest
	if err := ReadFrame(h.in, &m, MaxPluginMessage); err != nil {
		h.t.Fatalf("the host cannot read: %v", err)
	}
	return &m
}

func (h *fakeHost) write(m *pluginv1.ConnectResponse) {
	h.t.Helper()
	if err := WriteFrame(h.out, m); err != nil {
		h.t.Fatalf("the host cannot write: %v", err)
	}
}

func netAnswer(r *pluginv1.NetResponse) *pluginv1.ConnectResponse {
	return &pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Net{Net: r}}
}

func TestStdioSaysHelloThenGetsTheWelcome(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	hello := &pluginv1.Hello{Name: "Net check-in", Version: "0.4.1", Ids: []*pluginv1.IDRange{{First: 9100, Last: 9100}}}
	type result struct {
		w   *pluginv1.Welcome
		err error
	}
	done := make(chan result, 1)
	go func() {
		w, err := handshake(h.s, hello)
		done <- result{w, err}
	}()
	got := h.read().GetHello()
	if got.GetName() != "Net check-in" || got.GetVersion() != "0.4.1" || len(got.GetIds()) != 1 {
		t.Fatalf("the first message is %v, want the hello", got)
	}
	h.write(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Welcome{Welcome: &pluginv1.Welcome{Name: "Net check-in"}}})
	r := <-done
	if r.err != nil || r.w.GetName() != "Net check-in" {
		t.Fatalf("handshake = %v, %v", r.w, r.err)
	}
}

func TestStdioRefusesAnotherFirstMessage(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	done := make(chan error, 1)
	go func() {
		_, err := handshake(h.s, &pluginv1.Hello{Name: "x"})
		done <- err
	}()
	h.read()
	h.write(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Ranges{Ranges: &pluginv1.Ranges{}}})
	if err := <-done; !errors.Is(err, ErrNoWelcome) {
		t.Fatalf("handshake = %v, want ErrNoWelcome", err)
	}
}

func TestStdioKeepsTheOrderOfABacklog(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	const n = 600
	wrote := make(chan struct{})
	go func() {
		defer close(wrote)
		for i := range n {
			h.write(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: &pluginv1.Burst{Sequence: uint32(i % 256), StreamId: uint32(i + 1)}}})
		}
	}()
	select {
	case <-wrote:
	case <-time.After(10 * time.Second):
		t.Fatal("the host could not write while the plugin was not reading")
	}
	for i := range n {
		m, err := h.s.recv()
		if err != nil {
			t.Fatal(err)
		}
		if m.GetBurst().GetStreamId() != uint32(i+1) {
			t.Fatalf("message %d is stream %d", i, m.GetBurst().GetStreamId())
		}
	}
}

func TestStdioAnswersTheNetworkBehindABacklog(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	type result struct {
		err error
	}
	done := make(chan result, 1)
	go func() {
		_, err := h.s.dial(context.Background(), "nets.example.org:443")
		done <- result{err}
	}()
	open := h.read().GetNet()
	if open.GetOpen() != "nets.example.org:443" {
		t.Fatalf("the plugin asked %v, want an open of nets.example.org:443", open)
	}
	for i := range 600 {
		h.write(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: &pluginv1.Burst{StreamId: uint32(i + 1)}}})
	}
	h.write(netAnswer(&pluginv1.NetResponse{Conn: open.GetConn(), Op: pluginv1.NetOp_NET_OP_OPEN}))
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatal(r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the open got no answer while bursts waited for the plugin")
	}
}

func TestStdioDialReadsWritesAndCloses(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	go func() {
		open := h.read().GetNet()
		id := open.GetConn()
		h.write(netAnswer(&pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_OPEN}))
		w := h.read().GetNet()
		h.write(netAnswer(&pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_WRITE, Written: uint32(len(w.GetWrite()))}))
		r := h.read().GetNet()
		if r.GetRead() == 0 || r.GetRead() > MaxNetChunk {
			h.t.Errorf("read of %d bytes", r.GetRead())
		}
		h.write(netAnswer(&pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_READ, Data: []byte("echo " + string(w.GetWrite()))}))
		h.read()
		h.write(netAnswer(&pluginv1.NetResponse{Conn: id, Op: pluginv1.NetOp_NET_OP_READ, Eof: true}))
		if h.read().GetNet().GetClose() == nil {
			h.t.Error("the last request is not a close")
		}
	}()
	c, err := h.s.dial(context.Background(), "nets.example.org:443")
	if err != nil {
		t.Fatal(err)
	}
	if n, err := c.Write([]byte("hi")); err != nil || n != 2 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	got, err := io.ReadAll(c)
	if err != nil || string(got) != "echo hi" {
		t.Fatalf("ReadAll = %q, %v", got, err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestStdioDialFailsWithTheHostsReason(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	go func() {
		open := h.read().GetNet()
		h.write(netAnswer(&pluginv1.NetResponse{Conn: open.GetConn(), Op: pluginv1.NetOp_NET_OP_OPEN, Error: "not allowed"}))
	}()
	_, err := h.s.dial(context.Background(), "evil.example.org:25")
	if err == nil || !bytes.Contains([]byte(err.Error()), []byte("not allowed")) {
		t.Fatalf("dial = %v, want the host's reason", err)
	}
}

func TestStdioReadDeadlineKeepsTheLateBytes(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	reads := make(chan *pluginv1.NetRequest, 2)
	go func() {
		open := h.read().GetNet()
		h.write(netAnswer(&pluginv1.NetResponse{Conn: open.GetConn(), Op: pluginv1.NetOp_NET_OP_OPEN}))
		reads <- h.read().GetNet()
	}()
	c, err := h.s.dial(context.Background(), "nets.example.org:443")
	if err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	buf := make([]byte, 64)
	if _, err := c.Read(buf); !errors.Is(err, os.ErrDeadlineExceeded) {
		t.Fatalf("Read = %v, want the deadline", err)
	}
	r := <-reads
	h.write(netAnswer(&pluginv1.NetResponse{Conn: r.GetConn(), Op: pluginv1.NetOp_NET_OP_READ, Data: []byte("late")}))
	_ = c.SetReadDeadline(time.Time{})
	n, err := c.Read(buf)
	if err != nil || string(buf[:n]) != "late" {
		t.Fatalf("Read = %q, %v, want the late bytes", buf[:n], err)
	}
}

func TestStdioEndsWhenTheHostCloses(t *testing.T) {
	t.Parallel()
	h := newFakeHost(t)
	_ = h.out.Close()
	if _, err := h.s.recv(); err == nil {
		t.Fatal("recv works after the host closed")
	}
	if _, err := h.s.dial(context.Background(), "nets.example.org:443"); err == nil {
		t.Fatal("dial works after the host closed")
	}
}

func TestReadFrameRefusesAnOversizedLength(t *testing.T) {
	t.Parallel()
	var b bytes.Buffer
	_ = binary.Write(&b, binary.BigEndian, uint32(MaxPluginMessage+1))
	var m pluginv1.ConnectRequest
	if err := ReadFrame(&b, &m, MaxPluginMessage); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("ReadFrame = %v, want ErrFrameTooLarge", err)
	}
}
