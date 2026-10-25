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
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"errors"
	"math/big"
	"net"
	"sync"
	"testing"
	"time"

	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// scriptedHub serves each stream with the next of its scripts.
type scriptedHub struct {
	pluginv1.UnimplementedPluginServiceServer
	mu      sync.Mutex
	scripts []func(pluginv1.PluginService_ConnectServer) error
}

func (h *scriptedHub) Connect(stream pluginv1.PluginService_ConnectServer) error {
	h.mu.Lock()
	if len(h.scripts) == 0 {
		h.mu.Unlock()
		return errRefused
	}
	next := h.scripts[0]
	h.scripts = h.scripts[1:]
	h.mu.Unlock()
	return next(stream)
}

func startHub(t *testing.T, scripts ...func(pluginv1.PluginService_ConnectServer) error) Options {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{SerialNumber: big.NewInt(1), NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour)}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(der)
	var lc net.ListenConfig
	lis, err := lc.Listen(context.Background(), "tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := grpc.NewServer(grpc.Creds(credentials.NewTLS(&tls.Config{
		Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS13,
	})))
	pluginv1.RegisterPluginServiceServer(srv, &scriptedHub{scripts: scripts})
	go func() { _ = srv.Serve(lis) }()
	t.Cleanup(srv.Stop)
	return Options{Address: lis.Addr().String(), Token: "t", Fingerprint: hex.EncodeToString(sum[:])}
}

// statusError ends a stream of the test's hub with a gRPC code.
type statusError codes.Code

func (e statusError) Error() string {
	return codes.Code(e).String()
}

// GRPCStatus gives gRPC the code of e.
func (e statusError) GRPCStatus() *status.Status {
	return status.New(codes.Code(e), e.Error())
}

var (
	errRefused  = statusError(codes.Unavailable)
	errReplaced = statusError(codes.Aborted)
	errTimeout  = errors.New("Recv did not return")
)

func welcome(s pluginv1.PluginService_ConnectServer) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Welcome{Welcome: &pluginv1.Welcome{Name: "P"}}})
}

func sendBurst(s pluginv1.PluginService_ConnectServer, seq uint32) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Burst{Burst: &pluginv1.Burst{
		Source: 3191868, Destination: 9990, Slot: 1, StreamId: 7, Sequence: seq,
	}}})
}

func draining(s pluginv1.PluginService_ConnectServer) {
	_ = s.Send(&pluginv1.ConnectResponse{Message: &pluginv1.ConnectResponse_Draining{Draining: &pluginv1.Draining{}}})
}

func refused(pluginv1.PluginService_ConnectServer) error {
	return errRefused
}

func recvWithin(t *testing.T, c *Conn) (*pluginv1.ConnectResponse, error) {
	t.Helper()
	type res struct {
		m   *pluginv1.ConnectResponse
		err error
	}
	ch := make(chan res, 1)
	go func() {
		m, err := c.Recv()
		ch <- res{m, err}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(5 * time.Second):
		t.Fatal(errTimeout)
		return nil, errTimeout
	}
}

func TestAConnMovesToANewStreamOnDraining(t *testing.T) {
	t.Parallel()
	newUp := make(chan struct{})
	newGot := make(chan *pluginv1.ConnectRequest, 8)
	opts := startHub(t,
		func(s pluginv1.PluginService_ConnectServer) error {
			welcome(s)
			_, _ = s.Recv()
			sendBurst(s, 1)
			draining(s)
			<-newUp
			sendBurst(s, 2)
			sendBurst(s, 3)
			time.Sleep(100 * time.Millisecond)
			return errReplaced
		},
		refused,
		func(s pluginv1.PluginService_ConnectServer) error {
			welcome(s)
			m, _ := s.Recv()
			newGot <- m
			close(newUp)
			sendBurst(s, 2)
			sendBurst(s, 4)
			m, _ = s.Recv()
			newGot <- m
			<-s.Context().Done()
			return nil
		},
	)
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	if err := c.SubscribeTracking(true); err != nil {
		t.Fatal(err)
	}
	seen := map[uint32]int{}
	for len(seen) < 4 {
		m, err := recvWithin(t, c)
		if err != nil {
			t.Fatalf("Recv: %v", err)
		}
		if m.GetBurst() == nil {
			t.Fatalf("got %v, want only bursts", m)
		}
		seen[m.GetBurst().GetSequence()]++
	}
	first := <-newGot
	if !first.GetSubscribe().GetTracking() {
		t.Fatalf("the new stream got %v first, want the tracking subscription", first)
	}
	if err := c.Send(&pluginv1.Burst{StreamId: 99}); err != nil {
		t.Fatal(err)
	}
	select {
	case m := <-newGot:
		if m.GetBurst().GetStreamId() != 99 {
			t.Fatalf("the new stream got %v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a burst sent after the move did not reach the new stream")
	}
	time.Sleep(300 * time.Millisecond)
	for seq := uint32(1); seq <= 4; seq++ {
		if seen[seq] != 1 {
			t.Fatalf("burst %d came %d times: %v", seq, seen[seq], seen)
		}
	}
	select {
	case <-c.done:
		t.Fatal("the connection closed")
	default:
	}
	c.mu.Lock()
	failed := c.failed
	c.mu.Unlock()
	if failed != nil {
		t.Fatalf("the connection failed: %v", failed)
	}
}

func TestAConnEndsAsBeforeWhenTheHubSendsNoDraining(t *testing.T) {
	t.Parallel()
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcome(s)
		sendBurst(s, 1)
		return errRefused
	})
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	m, err := recvWithin(t, c)
	if err != nil || m.GetBurst().GetSequence() != 1 {
		t.Fatalf("got %v, %v", m, err)
	}
	for range 2 {
		_, err = recvWithin(t, c)
		if status.Code(errors.Unwrap(err)) != codes.Unavailable {
			t.Fatalf("got %v, want the end of the stream", err)
		}
	}
}

func TestAConnEndsWhenItCannotMoveAndTheOldStreamEnds(t *testing.T) {
	t.Parallel()
	opts := startHub(t, func(s pluginv1.PluginService_ConnectServer) error {
		welcome(s)
		draining(s)
		return errRefused
	})
	c, err := Dial(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	c.moveFor = 500 * time.Millisecond
	c.moveEvery = 50 * time.Millisecond
	_, err = recvWithin(t, c)
	if status.Code(errors.Unwrap(err)) != codes.Unavailable {
		t.Fatalf("got %v, want the end of the old stream", err)
	}
}
