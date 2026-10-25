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

package plugins_test

import (
	"context"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
)

// replica is another replica of the hub on the rig's database.
type replica struct {
	t   *testing.T
	svc *plugins.Service
	lis *bufconn.Listener
}

func (r *rig) replica(ps pubsub.PubSub) *replica {
	r.t.Helper()
	x := &replica{t: r.t, svc: plugins.NewService(r.db, ps, r.registry, r.router, nil), lis: bufconn.Listen(1 << 20)}
	srv := grpc.NewServer()
	pluginv1.RegisterPluginServiceServer(srv, x.svc)
	go func() { _ = srv.Serve(x.lis) }()
	r.t.Cleanup(srv.Stop)
	return x
}

func (x *replica) connect(token string) pluginv1.PluginService_ConnectClient {
	x.t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return x.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(x.t, err)
	x.t.Cleanup(func() { _ = conn.Close() })
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
	stream, err := pluginv1.NewPluginServiceClient(conn).Connect(ctx)
	require.NoError(x.t, err)
	return stream
}

func drain(svc *plugins.Service, d time.Duration) <-chan int {
	left := make(chan int, 1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), d)
		defer cancel()
		left <- svc.Drain(ctx)
	}()
	return left
}

func TestADrainingReplicaTellsItsPluginsToMoveAndRefusesNewStreams(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	_, token := r.add("Parrot", rng(9990, 9990))
	old := r.replica(r.ps)
	a := old.connect(token)
	_, err := recv(t, a)
	require.NoError(t, err)

	left := drain(old.svc, 10*time.Second)
	m, err := recv(t, a)
	require.NoError(t, err)
	require.NotNil(t, m.GetDraining(), "the plugin learns that the replica stops")
	_, err = recv(t, old.connect(token))
	assert.Equal(t, codes.Unavailable, status.Code(err), "a draining replica takes no new stream")

	b := r.connect(token)
	m, err = recv(t, b)
	require.NoError(t, err)
	require.NotNil(t, m.GetWelcome())
	for {
		_, err = recv(t, a)
		if err != nil {
			break
		}
	}
	assert.Equal(t, codes.Aborted, status.Code(err), "the new stream ends the old one")
	select {
	case n := <-left:
		assert.Equal(t, 0, n)
	case <-time.After(5 * time.Second):
		t.Fatal("the drain did not end when the stream moved")
	}
}

func TestADrainEndsAtItsDeadlineWhenAPluginStays(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	a := r.connect(token)
	_, err := recv(t, a)
	require.NoError(t, err)
	assert.Equal(t, 1, <-drain(r.svc, 300*time.Millisecond))
	m, err := recv(t, a)
	require.NoError(t, err)
	require.NotNil(t, m.GetDraining())
	r.registry.Deliver(p.ID, models.Packet{Src: 3191868, Dst: 9990, StreamID: 5})
	m, err = recv(t, a)
	require.NoError(t, err)
	assert.Equal(t, uint32(5), m.GetBurst().GetStreamId(), "the stream works until the replica stops")
}

// slowIn delays the bursts to the plugins, as a busy subscription does.
type slowIn struct {
	pubsub.PubSub
	delay time.Duration
}

type slowSub struct {
	pubsub.Subscription
	ch chan []byte
}

func (s slowSub) Channel() <-chan []byte {
	return s.ch
}

func (s slowIn) Subscribe(topic string) pubsub.Subscription {
	sub := s.PubSub.Subscribe(topic)
	if !strings.HasPrefix(topic, "plugins:in:") {
		return sub
	}
	out := slowSub{Subscription: sub, ch: make(chan []byte, 16)}
	go func() {
		defer close(out.ch)
		for b := range sub.Channel() {
			time.Sleep(s.delay)
			out.ch <- b
		}
	}()
	return out
}

func TestAStreamOfADrainingReplicaSendsTheBurstsThatCameBeforeTheNewStream(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	old := r.replica(slowIn{PubSub: r.ps, delay: 300 * time.Millisecond})
	a := old.connect(token)
	_, err := recv(t, a)
	require.NoError(t, err)
	drain(old.svc, 10*time.Second)
	m, err := recv(t, a)
	require.NoError(t, err)
	require.NotNil(t, m.GetDraining())

	r.registry.Deliver(p.ID, models.Packet{Src: 3191868, Dst: 9990, StreamID: 6})
	b := r.connect(token)
	_, err = recv(t, b)
	require.NoError(t, err)
	m, err = recv(t, a)
	require.NoError(t, err, "the old stream sends the burst before it ends")
	assert.Equal(t, uint32(6), m.GetBurst().GetStreamId())
	_, err = recv(t, a)
	assert.Equal(t, codes.Aborted, status.Code(err))
}
