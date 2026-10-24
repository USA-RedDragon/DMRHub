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
	"errors"
	"fmt"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/gorm"
)

var errNoMessage = errors.New("no message from the hub")

type routed struct {
	mu   sync.Mutex
	pkts []models.Packet
	srcs []string
}

func (r *routed) RoutePacket(_ context.Context, p models.Packet, source string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pkts = append(r.pkts, p)
	r.srcs = append(r.srcs, source)
}

func (r *routed) all() ([]models.Packet, []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]models.Packet(nil), r.pkts...), append([]string(nil), r.srcs...)
}

type rig struct {
	t        *testing.T
	db       *gorm.DB
	ps       pubsub.PubSub
	registry *plugins.Registry
	router   *routed
	lis      *bufconn.Listener
}

func newRig(t *testing.T) *rig {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_plugins_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	registry, err := plugins.NewRegistry(database, ps)
	require.NoError(t, err)
	done := make(chan struct{})
	registry.Follow(done)
	r := &rig{t: t, db: database, ps: ps, registry: registry, router: &routed{}, lis: bufconn.Listen(1 << 20)}
	srv := grpc.NewServer()
	pluginv1.RegisterPluginServiceServer(srv, plugins.NewService(database, ps, registry, r.router))
	go func() { _ = srv.Serve(r.lis) }()
	t.Cleanup(func() {
		srv.Stop()
		close(done)
		_ = ps.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})
	return r
}

func (r *rig) add(name string, ranges ...models.PluginRange) (models.Plugin, string) {
	r.t.Helper()
	p, token, err := models.CreatePlugin(r.db, name, ranges, time.Now())
	require.NoError(r.t, err)
	require.NoError(r.t, r.registry.Reload())
	return p, token
}

func (r *rig) changed() {
	r.t.Helper()
	require.NoError(r.t, plugins.PublishChanged(r.ps))
}

func (r *rig) connect(token string) pluginv1.PluginService_ConnectClient {
	r.t.Helper()
	conn, err := grpc.NewClient("passthrough:///bufnet",
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return r.lis.DialContext(ctx) }),
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(r.t, err)
	r.t.Cleanup(func() { _ = conn.Close() })
	ctx := metadata.AppendToOutgoingContext(context.Background(), "authorization", "Bearer "+token)
	stream, err := pluginv1.NewPluginServiceClient(conn).Connect(ctx)
	require.NoError(r.t, err)
	return stream
}

func recv(t *testing.T, s pluginv1.PluginService_ConnectClient) (*pluginv1.ConnectResponse, error) {
	t.Helper()
	type res struct {
		m   *pluginv1.ConnectResponse
		err error
	}
	ch := make(chan res, 1)
	go func() {
		m, err := s.Recv()
		ch <- res{m, err}
	}()
	select {
	case r := <-ch:
		return r.m, r.err
	case <-time.After(3 * time.Second):
		t.Fatal("no message from the hub")
		return nil, errNoMessage
	}
}

func rng(first, last uint) models.PluginRange {
	return models.PluginRange{First: first, Last: last}
}

func burst(src, dst uint32, group bool) *pluginv1.Burst {
	return &pluginv1.Burst{Source: src, Destination: dst, GroupCall: group, Slot: 2, StreamId: 77, FrameType: pluginv1.FrameType_FRAME_TYPE_VOICE,
		DataTypeOrVoiceSequence: 3, Sequence: 9, Payload: make([]byte, 33)}
}

func TestAPluginNeedsAValidTokenAndToBeOn(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	_, err := recv(t, r.connect("dmrhub_plg_nope"))
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
	_, err = models.UpdatePlugin(r.db, p.ID, "Parrot", false, p.Ranges)
	require.NoError(t, err)
	_, err = recv(t, r.connect(token))
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
}

func TestAPluginIsWelcomedWithItsNameAndIDs(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Weather", rng(310001, 310009), rng(9998, 9998))
	m, err := recv(t, r.connect(token))
	require.NoError(t, err)
	w := m.GetWelcome()
	require.NotNil(t, w)
	assert.Equal(t, "Weather", w.GetName())
	require.Len(t, w.GetRanges(), 2)
	assert.Equal(t, uint32(9998), w.GetRanges()[0].GetFirst())
	got, err := models.FindPlugin(r.db, p.ID)
	require.NoError(t, err)
	assert.NotNil(t, got.LastConnectedAt)
}

func TestTheBurstsToAPluginsIDsReachIt(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	s := r.connect(token)
	_, err := recv(t, s)
	require.NoError(t, err)
	owner, ok := r.registry.Owner(9990)
	require.True(t, ok)
	assert.Equal(t, p.ID, owner)
	_, ok = r.registry.Owner(9991)
	assert.False(t, ok)

	pkt := models.Packet{Src: 3191868, Dst: 9990, Slot: true, StreamID: 5, FrameType: dmrconst.FrameVoiceSync, DTypeOrVSeq: 0, Seq: 1, Repeater: 319186801}
	pkt.DMRData[0] = 0xAB
	r.registry.Deliver(p.ID, pkt)
	m, err := recv(t, s)
	require.NoError(t, err)
	b := m.GetBurst()
	require.NotNil(t, b)
	assert.Equal(t, uint32(3191868), b.GetSource())
	assert.Equal(t, uint32(9990), b.GetDestination())
	assert.Equal(t, uint32(2), b.GetSlot())
	assert.Equal(t, pluginv1.FrameType_FRAME_TYPE_VOICE_SYNC, b.GetFrameType())
	assert.Equal(t, uint32(319186801), b.GetRepeater())
	assert.Equal(t, byte(0xAB), b.GetPayload()[0])
}

func TestAPluginSendsOnlyFromItsIDsAndNeverToAnotherPlugin(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Weather", rng(310001, 310009))
	r.add("Parrot", rng(9990, 9990))
	s := r.connect(token)
	_, err := recv(t, s)
	require.NoError(t, err)

	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: burst(310001, 3191868, false)}}))
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: burst(310002, 310009, true)}}))
	require.Eventually(t, func() bool { pkts, _ := r.router.all(); return len(pkts) == 2 }, 3*time.Second, 10*time.Millisecond)
	pkts, srcs := r.router.all()
	assert.Equal(t, uint(310001), pkts[0].Src)
	assert.Equal(t, uint(3191868), pkts[0].Dst)
	assert.True(t, pkts[0].Slot)
	assert.Equal(t, dmrconst.FrameVoice, pkts[0].FrameType)
	assert.Equal(t, string(dmrconst.CommandDMRD), pkts[0].Signature)
	assert.Equal(t, plugins.SourceName(p.ID), srcs[0])
	assert.True(t, pkts[1].GroupCall)

	bad := map[pluginv1.RefusalReason]*pluginv1.Burst{
		pluginv1.RefusalReason_REFUSAL_REASON_SOURCE:      burst(3191868, 3140598, false),
		pluginv1.RefusalReason_REFUSAL_REASON_DESTINATION: burst(310001, 91, true),
	}
	toParrot := burst(310001, 9990, false)
	short := burst(310001, 3191868, false)
	short.Payload = short.GetPayload()[:20]
	noFrame := burst(310001, 3191868, false)
	noFrame.FrameType = pluginv1.FrameType_FRAME_TYPE_UNSPECIFIED
	slot3 := burst(310001, 3191868, false)
	slot3.Slot = 3
	for reason, b := range bad {
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: b}}))
		m, err := recv(t, s)
		require.NoError(t, err)
		assert.Equal(t, reason, m.GetRefusal().GetReason())
	}
	for want, b := range map[*pluginv1.Burst]pluginv1.RefusalReason{
		toParrot: pluginv1.RefusalReason_REFUSAL_REASON_DESTINATION,
		short:    pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED,
		noFrame:  pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED,
		slot3:    pluginv1.RefusalReason_REFUSAL_REASON_MALFORMED,
	} {
		require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: want}}))
		m, err := recv(t, s)
		require.NoError(t, err)
		assert.Equal(t, b, m.GetRefusal().GetReason())
		assert.Equal(t, uint32(77), m.GetRefusal().GetStreamId())
	}
	pkts, _ = r.router.all()
	assert.Len(t, pkts, 2, "nothing refused was routed")
}

func TestANewerConnectionOfAPluginEndsTheOlder(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	_, token := r.add("Parrot", rng(9990, 9990))
	old := r.connect(token)
	_, err := recv(t, old)
	require.NoError(t, err)
	fresh := r.connect(token)
	_, err = recv(t, fresh)
	require.NoError(t, err)
	_, err = recv(t, old)
	assert.Equal(t, codes.Aborted, status.Code(err))
}

func TestAdminChangesReachAConnectedPlugin(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	s := r.connect(token)
	_, err := recv(t, s)
	require.NoError(t, err)

	_, err = models.UpdatePlugin(r.db, p.ID, "Parrot", true, []models.PluginRange{rng(9990, 9991)})
	require.NoError(t, err)
	r.changed()
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetRanges())
	assert.Equal(t, uint32(9991), m.GetRanges().GetRanges()[0].GetLast())
	require.Eventually(t, func() bool { _, ok := r.registry.Owner(9991); return ok }, 3*time.Second, 10*time.Millisecond)

	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Burst{Burst: burst(9991, 3191868, false)}}))
	require.Eventually(t, func() bool { pkts, _ := r.router.all(); return len(pkts) == 1 }, 3*time.Second, 10*time.Millisecond, "the new IDs are the plugin's")

	_, err = models.RotatePluginToken(r.db, p.ID, time.Now())
	require.NoError(t, err)
	r.changed()
	_, err = recv(t, s)
	assert.Equal(t, codes.Unauthenticated, status.Code(err))
}

func TestTurningAPluginOffEndsItsStream(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	s := r.connect(token)
	_, err := recv(t, s)
	require.NoError(t, err)
	_, err = models.UpdatePlugin(r.db, p.ID, "Parrot", false, p.Ranges)
	require.NoError(t, err)
	r.changed()
	_, err = recv(t, s)
	assert.Equal(t, codes.PermissionDenied, status.Code(err))
	require.Eventually(t, func() bool { _, ok := r.registry.Owner(9990); return !ok }, 3*time.Second, 10*time.Millisecond, "a plugin that is off owns no IDs")
}

func online(t *testing.T, r *rig, id uint) bool {
	t.Helper()
	p, err := models.FindPlugin(r.db, id)
	require.NoError(t, err)
	return p.Online(time.Now())
}

func TestAPluginIsOnlineWhileItsLatestStreamLasts(t *testing.T) {
	t.Parallel()
	r := newRig(t)
	p, token := r.add("Parrot", rng(9990, 9990))
	assert.False(t, online(t, r, p.ID))
	old := r.connect(token)
	_, err := recv(t, old)
	require.NoError(t, err)
	assert.True(t, online(t, r, p.ID))

	fresh := r.connect(token)
	_, err = recv(t, fresh)
	require.NoError(t, err)
	_, err = recv(t, old)
	require.Equal(t, codes.Aborted, status.Code(err))
	time.Sleep(200 * time.Millisecond)
	assert.True(t, online(t, r, p.ID), "the stream that was replaced does not mark the newer one offline")

	require.NoError(t, fresh.CloseSend())
	require.Eventually(t, func() bool { return !online(t, r, p.ID) }, 3*time.Second, 20*time.Millisecond)
}
