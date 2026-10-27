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
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	pcOwner   = uint(3000001)
	pcOther   = uint(3000002)
	pcVisitor = uint(3000005)
	pcNever   = uint(3000007)
	pcOwnHS   = uint(300001)
	pcOtherHS = uint(300002)
	pcThirdHS = uint(300003)
)

// privateHub returns a hub that routes private traffic with pc. pcOwner
// owns the hotspot pcOwnHS, and pcOther owns pcOtherHS and pcThirdHS,
// which no admin trusts.
func privateHub(t *testing.T, pc config.PrivateCalls) (*hub.Hub, *hub.ServerHandle) {
	t.Helper()
	h, database, _ := makeTestHubWithPubSub(t)
	h.SetConfig(config.Config{DMR: config.DMR{PrivateCalls: pc}})
	seedUser(t, database, pcOwner, "OWNER")
	seedUser(t, database, pcOther, "OTHER")
	seedRepeater(t, database, pcOwnHS, pcOwner)
	seedRepeater(t, database, pcOtherHS, pcOther)
	seedRepeater(t, database, pcThirdHS, pcOther)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	for _, id := range []uint{pcOwnHS, pcOtherHS, pcThirdHS} {
		h.ActivateRepeater(context.Background(), id)
	}
	return h, srv
}

func privateVoice(src, dst, repeater, stream uint) models.Packet {
	p := makeVoicePacket(dst, stream, false, true)
	p.Src, p.Repeater = src, repeater
	return p
}

// hear routes a data header of radio on repeater, and waits until the
// record in the KV store of where the radio was heard has it.
func hear(t *testing.T, h *hub.Hub, radio, repeater, stream uint) {
	t.Helper()
	require.Eventually(t, func() bool { return h.LookupsLoaded(nil, []uint{radio}) }, 30*time.Second, 10*time.Millisecond, "the hub did not load the owner of radio %d", radio)
	h.RoutePacket(context.Background(), dataHeader(radio, 1, repeater, stream), models.RepeaterTypeMMDVM)
	require.Eventually(t, func() bool { return h.HeardLastOn(context.Background(), radio) == repeater }, 30*time.Second, 10*time.Millisecond, "the hub did not store that repeater %d heard radio %d", repeater, radio)
}

func TestHybridReachesAVisitorWhereverItWasHeard(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{})
	hear(t, h, pcVisitor, pcOtherHS, 1)

	h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, pcOtherHS, "a voice call to a radio with no account goes where it was heard")
	h.RoutePacket(context.Background(), dataHeader(pcOwner, pcVisitor, pcOwnHS, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, pcOtherHS, "data to a radio with no account goes where it was heard")
}

func TestMembersReachDropsAVisitor(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{Reach: config.PrivateReachMembers}})
	hear(t, h, pcVisitor, pcOtherHS, 1)

	h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "the members reach drops a call to a radio with no account")
}

func TestHybridDoesNotFollowAnOwnedRadioToAnUntrustedHotspot(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{})
	hear(t, h, pcOwner, pcOwnHS, 1)
	hear(t, h, pcOwner, pcOtherHS, 2)

	h.RoutePacket(context.Background(), privateVoice(pcOther, pcOwner, pcThirdHS, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, pcOwnHS, "a spoofed ID on another user's hotspot must not pull the calls of an owned radio")
}

func TestAnyoneFollowsAnOwnedRadioToAnyHotspot(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{Reach: config.PrivateReachAnyone}})
	hear(t, h, pcOwner, pcOwnHS, 1)
	hear(t, h, pcOwner, pcOtherHS, 2)

	h.RoutePacket(context.Background(), privateVoice(pcOther, pcOwner, pcThirdHS, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, pcOtherHS, "the anyone reach follows a radio to wherever it was last heard")
}

func TestUnknownTargetIsDroppedByDefault(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{})

	h.RoutePacket(context.Background(), privateVoice(pcOwner, pcNever, pcOwnHS, 1), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "a call to a radio that was never heard")
}

func TestUnknownTargetFloodsEveryOtherRepeater(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{Unknown: config.PrivateUnknownFlood}})

	for seq := range uint(2) {
		p := privateVoice(pcOwner, pcNever, pcOwnHS, 1)
		p.Seq = seq
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		got := map[uint]int{}
		for range 2 {
			select {
			case rp := <-srv.Packets:
				got[rp.RepeaterID]++
			case <-time.After(2 * time.Second):
				t.Fatalf("burst %d reached only %v", seq, got)
			}
		}
		assert.Equal(t, map[uint]int{pcOtherHS: 1, pcThirdHS: 1}, got, "burst %d floods every repeater but its own", seq)
		expectNone(t, srv, "the source repeater does not get its own burst back")
	}
}

func TestMaxAgeDropsAStaleSighting(t *testing.T) {
	t.Parallel()
	h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{MaxAge: 500 * time.Millisecond}})
	hear(t, h, pcVisitor, pcOtherHS, 1)

	h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, pcOtherHS, "a sighting within the max age")
	time.Sleep(time.Second)
	h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 3), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "a sighting older than the max age")
}

func TestDataTakesTheVoiceSettingsUnlessItHasItsOwn(t *testing.T) {
	t.Parallel()
	t.Run("voice reach", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{Reach: config.PrivateReachAnyone}})
		hear(t, h, pcOwner, pcOwnHS, 1)
		hear(t, h, pcOwner, pcOtherHS, 2)
		h.RoutePacket(context.Background(), dataHeader(pcOther, pcOwner, pcThirdHS, 3), models.RepeaterTypeMMDVM)
		expectAt(t, srv, pcOtherHS, "data takes the voice reach")
	})
	t.Run("own reach", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{Data: config.PrivateData{Reach: config.PrivateReachMembers}})
		hear(t, h, pcVisitor, pcOtherHS, 1)
		h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
		expectAt(t, srv, pcOtherHS, "voice keeps the hybrid reach")
		h.RoutePacket(context.Background(), dataHeader(pcOwner, pcVisitor, pcOwnHS, 3), models.RepeaterTypeMMDVM)
		expectNone(t, srv, "data takes its own members reach")
	})
}

func TestDataMaxAgeTakesTheVoiceMaxAgeOrNone(t *testing.T) {
	t.Parallel()
	voice := config.PrivateRoute{MaxAge: 500 * time.Millisecond}
	t.Run("voice max age", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{Voice: voice})
		hear(t, h, pcVisitor, pcOtherHS, 1)
		time.Sleep(time.Second)
		h.RoutePacket(context.Background(), dataHeader(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
		expectNone(t, srv, "data takes the voice max age")
	})
	t.Run("no limit", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{Voice: voice, Data: config.PrivateData{MaxAge: -time.Second}})
		hear(t, h, pcVisitor, pcOtherHS, 1)
		time.Sleep(time.Second)
		h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 2), models.RepeaterTypeMMDVM)
		expectNone(t, srv, "voice keeps its max age")
		h.RoutePacket(context.Background(), dataHeader(pcOwner, pcVisitor, pcOwnHS, 3), models.RepeaterTypeMMDVM)
		expectAt(t, srv, pcOtherHS, "a negative data max age is no limit")
	})
}

func TestTheLastingRecordLocatesARadioHeardLongAgo(t *testing.T) {
	t.Parallel()
	t.Run("no max age", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{})
		require.NoError(t, h.StoreSighting(pcVisitor, pcOtherHS, time.Now().Add(-48*time.Hour), true))
		h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 1), models.RepeaterTypeMMDVM)
		expectAt(t, srv, pcOtherHS, "a radio heard two days ago, with no max age")
	})
	t.Run("max age of a day", func(t *testing.T) {
		t.Parallel()
		h, srv := privateHub(t, config.PrivateCalls{Voice: config.PrivateRoute{MaxAge: 24 * time.Hour}})
		require.NoError(t, h.StoreSighting(pcVisitor, pcOtherHS, time.Now().Add(-48*time.Hour), true))
		h.RoutePacket(context.Background(), privateVoice(pcOwner, pcVisitor, pcOwnHS, 1), models.RepeaterTypeMMDVM)
		expectNone(t, srv, "a radio heard two days ago, with a max age of a day")
	})
}

func TestTheNewestSightingWins(t *testing.T) {
	t.Parallel()
	h, _ := privateHub(t, config.PrivateCalls{})
	now := time.Now()
	require.NoError(t, h.StoreSighting(pcVisitor, pcOtherHS, now, true))
	require.NoError(t, h.StoreSighting(pcVisitor, pcThirdHS, now.Add(-time.Minute), true))
	assert.Equal(t, pcOtherHS, h.HeardAt(context.Background(), pcVisitor), "an older sighting that comes late")
	require.NoError(t, h.StoreSighting(pcVisitor, pcThirdHS, now.Add(time.Minute), true))
	assert.Equal(t, pcThirdHS, h.HeardAt(context.Background(), pcVisitor), "a newer sighting")
}

// TestTheResyncKeepsTheFloodSubscription checks that the resync of a
// repeater, which runs every 10 s, does not take the subscription to the
// private flood for a change of talkgroups. A reload of the subscriptions
// lost bursts of the calls in progress.
func TestTheResyncKeepsTheFloodSubscription(t *testing.T) {
	t.Parallel()
	h, _ := privateHub(t, config.PrivateCalls{})
	for _, id := range []uint{pcOwnHS, pcOtherHS, pcThirdHS} {
		assert.False(t, h.ResyncRepeater(id), "repeater %d", id)
	}
}
