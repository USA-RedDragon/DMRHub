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

package hub_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdmitStreamKeepsAStreamWithItsFirstSource(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	burst := func(stream, repeater uint) models.Packet {
		p := makeVoicePacket(1, stream, true, false)
		p.Repeater = repeater
		return p
	}

	assert.True(t, h.AdmitStream(t.Context(), burst(42, 100001), models.RepeaterTypeMMDVM))
	assert.True(t, h.AdmitStream(t.Context(), burst(42, 100001), models.RepeaterTypeMMDVM), "the source of the stream keeps it, also after a reconnect")
	assert.False(t, h.AdmitStream(t.Context(), burst(42, 100002), models.RepeaterTypeMMDVM), "another repeater with the same stream is a loop")
	assert.False(t, h.AdmitStream(t.Context(), burst(42, 100001), "openbridge"), "a peer with the same ID on another server is another source")
	assert.False(t, h.AdmitStream(t.Context(), burst(42, 100001), models.RepeaterTypeIPSC))
	assert.True(t, h.AdmitStream(t.Context(), burst(42, 100001), models.RepeaterTypeMMDVM), "dropped copies do not take the stream over")
	assert.True(t, h.AdmitStream(t.Context(), burst(43, 100002), models.RepeaterTypeMMDVM), "other streams are not affected")
	other := burst(42, 100003)
	other.Src = 1000002
	assert.True(t, h.AdmitStream(t.Context(), other, models.RepeaterTypeMMDVM), "another radio may use the same stream ID")
	assert.True(t, h.AdmitStream(t.Context(), burst(0, 100001), models.RepeaterTypeMMDVM))
	assert.True(t, h.AdmitStream(t.Context(), burst(0, 100002), models.RepeaterTypeMMDVM), "stream 0 carries no stream identity")
}

func replicaHubs(t *testing.T, be testutils.Backend) (*hub.Hub, *hub.Hub) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	be.Setup(t, &cfg)
	shared, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	one, two := hub.NewHub(nil, shared, nil, nil), hub.NewHub(nil, shared, nil, nil)
	t.Cleanup(func() {
		one.Stop(context.Background())
		two.Stop(context.Background())
		_ = shared.Close()
	})
	return one, two
}

func forKVBackends(t *testing.T, f func(t *testing.T, be testutils.Backend)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, be)
		})
	}
}

func TestAdmitStreamKeepsAStreamWithItsFirstSourceAcrossReplicas(t *testing.T) {
	t.Parallel()
	forKVBackends(t, testFirstSourceAcrossReplicas)
}

func testFirstSourceAcrossReplicas(t *testing.T, be testutils.Backend) {
	one, two := replicaHubs(t, be)
	burst := func(repeater uint) models.Packet {
		p := makeVoicePacket(1, 42, true, false)
		p.Repeater = repeater
		return p
	}

	assert.True(t, one.AdmitStream(t.Context(), burst(100001), models.RepeaterTypeMMDVM))
	assert.False(t, two.AdmitStream(t.Context(), burst(100002), models.RepeaterTypeMMDVM), "another source on another replica is a loop")
	assert.True(t, two.AdmitStream(t.Context(), burst(100001), models.RepeaterTypeMMDVM), "the source keeps its stream when it moves to another replica")
	assert.True(t, one.AdmitStream(t.Context(), burst(100001), models.RepeaterTypeMMDVM))
	assert.False(t, one.AdmitStream(t.Context(), burst(100002), models.RepeaterTypeMMDVM))
}

func TestAStreamStaysWithItsSourceForTheHold(t *testing.T) {
	t.Parallel()
	assert.Equal(t, 30*time.Second, hub.StreamClaimHold)
	forKVBackends(t, testStaysForTheHold)
}

func testStaysForTheHold(t *testing.T, be testutils.Backend) {
	const hold, recheck = time.Second, 50 * time.Millisecond
	one, two := replicaHubs(t, be)
	one.SetStreamClaimTiming(hold, recheck)
	two.SetStreamClaimTiming(hold, recheck)
	ctx := t.Context()
	burst := func(repeater uint) models.Packet {
		p := makeVoicePacket(1, 42, true, false)
		p.Repeater = repeater
		return p
	}
	owner, copied := burst(100001), burst(100002)

	for start := time.Now(); time.Since(start) < 2*hold; time.Sleep(recheck) {
		require.True(t, one.AdmitStream(ctx, owner, models.RepeaterTypeMMDVM), "a call longer than the hold keeps its source")
		require.False(t, two.AdmitStream(ctx, copied, models.RepeaterTypeMMDVM), "a copy during the call is dropped")
		require.False(t, one.AdmitStream(ctx, copied, models.RepeaterTypeMMDVM))
	}
	last := time.Now()

	time.Sleep(hold * 2 / 5)
	assert.False(t, two.AdmitStream(ctx, copied, models.RepeaterTypeMMDVM), "a copy that loops back within the hold after the last burst is dropped")
	assert.False(t, one.AdmitStream(ctx, copied, models.RepeaterTypeMMDVM))

	time.Sleep(time.Until(last.Add(hold + recheck + 250*time.Millisecond)))
	assert.True(t, two.AdmitStream(ctx, copied, models.RepeaterTypeMMDVM), "after the hold with no burst from the source, dropped copies do not keep the claim")
	assert.False(t, one.AdmitStream(ctx, owner, models.RepeaterTypeMMDVM), "the new source owns the stream on every replica")
}

func TestASlotOfARepeaterIsCappedButPeersAreNot(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	h.LiftSlotRate(false)
	h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	admitted := func(server string, repeater uint, slot bool, n int) int {
		got := 0
		for i := range n {
			p := makeVoicePacket(1, uint(500+i), true, slot)
			p.Repeater = repeater
			if h.AdmitSlotRate(p, server) {
				got++
			}
		}
		return got
	}

	got := admitted(models.RepeaterTypeMMDVM, 100001, false, 200)
	assert.GreaterOrEqual(t, got, 50, "a slot may send in bursts")
	assert.LessOrEqual(t, got, 55, "200 bursts at once are far more than one slot can carry")
	assert.Equal(t, 50, admitted(models.RepeaterTypeMMDVM, 100001, true, 50), "the other slot has its own allowance")
	assert.Equal(t, 50, admitted(models.RepeaterTypeIPSC, 100001, false, 50), "the same ID on another server is another slot")
	assert.Equal(t, 2000, admitted("openbridge", 300001, false, 2000), "a peer carries many streams in bulk")
	time.Sleep(time.Second)
	assert.GreaterOrEqual(t, admitted(models.RepeaterTypeMMDVM, 100001, false, 40), 40, "the slot refills")
}

func TestSlotOverRateOnlyLooks(t *testing.T) {
	t.Parallel()
	h, _ := makeTestHub(t)
	h.LiftSlotRate(false)
	h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	assert.False(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false), "a slot that sent nothing has its allowance")
	for i := range 60 {
		p := makeVoicePacket(1, uint(900+i), true, false)
		p.Repeater = 100001
		h.AdmitSlotRate(p, models.RepeaterTypeMMDVM)
	}
	assert.True(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false))
	assert.False(t, h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, true), "the other slot is not over")
	assert.False(t, h.SlotOverRate("openbridge", 100001, false), "peers are never over")
	time.Sleep(100 * time.Millisecond)
	for range 100 {
		h.SlotOverRate(models.RepeaterTypeMMDVM, 100001, false)
	}
	p := makeVoicePacket(1, 999, true, false)
	p.Repeater = 100001
	assert.True(t, h.AdmitSlotRate(p, models.RepeaterTypeMMDVM), "looking takes none of the allowance")
}
