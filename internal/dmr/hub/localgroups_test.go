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
	"sync"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type localEnv struct {
	h        *hub.Hub
	db       *gorm.DB
	srv      *hub.ServerHandle
	group    uint
	mu       sync.Mutex
	refusals []string
}

// newLocalEnv makes a hub with TG 8 local, the local group North Texas on
// it with 100001 on TS2 and 100002 on TS1, 100003 asked to join it, and
// 100004 and 100003 on the network talkgroup 8 on TS2.
func newLocalEnv(t *testing.T) *localEnv {
	t.Helper()
	h, database := makeTestHub(t)
	e := &localEnv{h: h, db: database}
	h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{LocalTalkgroups: []uint{8, 9}}}})
	h.SetUndeliverableFunc(func(_ models.Packet, _, reason string) {
		e.mu.Lock()
		defer e.mu.Unlock()
		e.refusals = append(e.refusals, reason)
	})
	seedUser(t, database, 1000001, "OWNER")
	for _, id := range []uint{100001, 100002, 100003, 100004} {
		seedRepeater(t, database, id, 1000001)
	}
	seedTalkgroup(t, database, 8, "Network 8")
	for _, id := range []uint{100003, 100004} {
		var rpt models.Repeater
		require.NoError(t, database.First(&rpt, id).Error)
		require.NoError(t, database.Model(&rpt).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 8}))
	}
	g := models.LocalGroup{Name: "North Texas", Talkgroup: 8, OwnerID: 1000001}
	require.NoError(t, database.Create(&g).Error)
	e.group = g.ID
	for _, m := range []models.LocalGroupMember{
		{LocalGroupID: g.ID, RepeaterID: 100001, Slot: 2, State: models.LocalMemberIn},
		{LocalGroupID: g.ID, RepeaterID: 100002, Slot: 1, State: models.LocalMemberIn},
		{LocalGroupID: g.ID, RepeaterID: 100003, Slot: 2, State: models.LocalMemberAsked},
	} {
		require.NoError(t, models.AddLocalGroupMember(database, m))
	}
	e.srv = h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	for _, id := range []uint{100001, 100002, 100003, 100004} {
		h.ActivateRepeater(context.Background(), id)
	}
	knowTalkgroups(t, h)
	return e
}

// got returns the bursts of stream that each repeater got, by repeater.
func (e *localEnv) got(t *testing.T, like models.Packet, stream uint) map[uint][]models.Packet {
	t.Helper()
	out := map[uint][]models.Packet{}
	for _, id := range []uint{100001, 100002, 100003, 100004} {
		for _, rp := range flush(t, e.h, e.srv, like, id) {
			if rp.Packet.StreamID == stream {
				out[rp.RepeaterID] = append(out[rp.RepeaterID], rp.Packet)
			}
		}
	}
	return out
}

func (e *localEnv) refused() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.refusals...)
}

func TestALocalGroupCallGoesOnlyToTheOtherRepeatersOfTheGroup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)
		require.True(t, e.h.SubscribesLocal(100002, e.group))
		require.False(t, e.h.SubscribesLocal(100003, e.group), "a repeater only asked to join takes no calls")

		pkt := makeVoicePacket(8, 7001, true, true)
		e.h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		got := e.got(t, pkt, 7001)

		require.Len(t, got[100002], 1, "the other member gets the call")
		assert.False(t, got[100002][0].Slot, "on its own slot, TS1")
		assert.Equal(t, uint(100002), got[100002][0].Repeater)
		assert.Empty(t, got[100001], "the repeater the call came from does not get it back")
		assert.Empty(t, got[100003], "a repeater asked to join does not get it, nor through network TG 8")
		assert.Empty(t, got[100004], "the network talkgroup of the same number does not get it")
		assert.Empty(t, e.refused())

		rpt, err := models.FindRepeaterByID(e.db, 100001)
		require.NoError(t, err)
		assert.Nil(t, rpt.TS2DynamicTalkgroupID, "a local call links no dynamic talkgroup")
	})
}

func TestALocalCallOffTheGroupGoesNowhere(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)

		wrongSlot := makeVoicePacket(8, 7002, true, false)
		e.h.RoutePacket(context.Background(), wrongSlot, models.RepeaterTypeMMDVM)
		assert.Empty(t, e.got(t, wrongSlot, 7002), "a call on the other slot of a member goes nowhere")

		outside := makeVoicePacket(8, 7003, true, true)
		outside.Repeater = 100004
		e.h.RoutePacket(context.Background(), outside, models.RepeaterTypeMMDVM)
		assert.Empty(t, e.got(t, outside, 7003), "a call from a repeater in no group goes nowhere, not to network TG 8")

		assert.Equal(t, []string{undeliverable.ReasonNotInLocalGroup, undeliverable.ReasonNotInLocalGroup}, e.refused())
	})
}

func TestARepeaterTakesTheCallsOfALocalGroupOnceItJoins(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)

		require.NoError(t, models.SetLocalGroupMember(e.db, e.group, 100003, 0, true))
		e.h.ReloadRepeater(context.Background(), 100003)
		require.True(t, e.h.SubscribesLocal(100003, e.group))
		pkt := makeVoicePacket(8, 7004, true, true)
		e.h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		got := e.got(t, pkt, 7004)
		assert.Len(t, got[100003], 1, "the repeater that said yes gets the call once")
		assert.Len(t, got[100002], 1)

		require.NoError(t, models.RemoveLocalGroupMember(e.db, e.group, 100002))
		e.h.ReloadRepeater(context.Background(), 100002)
		assert.False(t, e.h.SubscribesLocal(100002, e.group))
		pkt.StreamID = 7005
		e.h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		got = e.got(t, pkt, 7005)
		assert.Empty(t, got[100002], "a repeater taken out of the group gets no more of its calls")
		assert.Len(t, got[100003], 1)
	})
}

func TestNoLocalTalkgroupsRoutesTheNumberAsANetworkTalkgroup(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)
		e.h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{DisableDynamicLinks: true}}})

		pkt := makeVoicePacket(8, 7006, true, true)
		e.h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
		got := e.got(t, pkt, 7006)
		assert.Empty(t, got[100002], "the group takes no calls while 8 is not local")
		assert.Len(t, got[100004], 1, "TG 8 is a network talkgroup again")
		assert.Empty(t, e.refused())
	})
}

func TestTheCallTrackerLearnsTheLocalGroupOfACall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)
		id, name, local := e.h.LocalGroupOf(100001, 8, true)
		assert.Equal(t, e.group, id)
		assert.Equal(t, "North Texas", name)
		assert.True(t, local)

		id, _, local = e.h.LocalGroupOf(100001, 8, false)
		assert.Zero(t, id, "the other slot of a member is in no group")
		assert.True(t, local, "TG 8 is still local there")
		id, _, local = e.h.LocalGroupOf(100004, 8, true)
		assert.Zero(t, id)
		assert.True(t, local)
		_, _, local = e.h.LocalGroupOf(100001, 91, true)
		assert.False(t, local)
	})
}

func TestALocalCallLinksAndUnlinksNothing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		e := newLocalEnv(t)
		seedTalkgroup(t, e.db, 91, "Chat")
		knowTalkgroups(t, e.h)
		dynamic := func(id uint) uint {
			rpt, err := models.FindRepeaterByID(e.db, id)
			require.NoError(t, err)
			if rpt.TS2DynamicTalkgroupID == nil {
				return 0
			}
			return *rpt.TS2DynamicTalkgroupID
		}

		chat := makeVoicePacket(91, 7101, true, true)
		e.h.RoutePacket(context.Background(), chat, models.RepeaterTypeMMDVM)
		settle(t, e.h, chat, models.RepeaterTypeMMDVM)
		require.Equal(t, uint(91), dynamic(100001), "a network call links its talkgroup")

		local := makeVoicePacket(8, 7102, true, true)
		e.h.RoutePacket(context.Background(), local, models.RepeaterTypeMMDVM)
		settle(t, e.h, local, models.RepeaterTypeMMDVM)
		assert.Equal(t, uint(91), dynamic(100001), "a call to the local group neither links TG 8 nor unlinks TG 91")

		outside := makeVoicePacket(8, 7103, true, true)
		outside.Repeater = 100004
		e.h.RoutePacket(context.Background(), outside, models.RepeaterTypeMMDVM)
		settle(t, e.h, outside, models.RepeaterTypeMMDVM)
		assert.Zero(t, dynamic(100004), "a dropped call to a local number links nothing")
	})
}
