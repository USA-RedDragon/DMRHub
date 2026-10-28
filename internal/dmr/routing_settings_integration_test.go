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

package dmr_test

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// routingStack makes the hub of stack run with r, and returns the config
// that the test changes while the hub runs.
func routingStack(t *testing.T, stack *testutils.IntegrationStack, r config.Routing) *config.Current {
	t.Helper()
	cfg := *stack.Config
	cfg.DMR.Routing = r
	cur := config.NewCurrent(&cfg)
	stack.Hub.SetConfig(cur)
	return cur
}

func setRouting(cur *config.Current, r config.Routing) {
	next := *cur.Get()
	next.DMR.Routing = r
	cur.Set(&next)
}

// oneHotspot seeds a user, talkgroups and the hotspot 100001 of the user,
// starts the servers and logs the hotspot in.
func oneHotspot(t *testing.T, stack *testutils.IntegrationStack, talkgroups ...uint) *testutils.MMDVMClient {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	for _, tg := range talkgroups {
		stack.SeedTalkgroup(t, tg, "TG")
	}
	stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
	stack.StartServers(t)
	c := testutils.NewMMDVMClient(100001, "", testPassword)
	require.NoError(t, c.Connect(stack.MMDVMAddr))
	t.Cleanup(c.Close)
	require.NoError(t, c.WaitReady(handshakeWait))
	time.Sleep(settleDuration)
	return c
}

// dynamicTS2 returns the dynamic talkgroup of TS2 of the hotspot 100001.
func dynamicTS2(t *testing.T, stack *testutils.IntegrationStack) *uint {
	t.Helper()
	rpt, err := models.FindRepeaterByID(stack.DB, 100001)
	require.NoError(t, err)
	return rpt.TS2DynamicTalkgroupID
}

// tgCall has c send a short call of radio 1000001 to dst on TS2.
func tgCall(t *testing.T, c *testutils.MMDVMClient, dst, stream uint) {
	t.Helper()
	require.NoError(t, c.SendDMRD(makeGroupVoicePacket(1000001, dst, stream, true)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, c.SendDMRD(makeGroupVoiceTermPacket(1000001, dst, stream, true)))
}

func TestDynamicTalkgroupTimesOut(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{DynamicTimeout: 600 * time.Millisecond})
		c := oneHotspot(t, stack, 3100)

		tgCall(t, c, 3100, 501)
		require.Eventually(t, func() bool { return dynamicTS2(t, stack) != nil }, 5*time.Second, 20*time.Millisecond,
			"a call on a talkgroup links it")
		require.Eventually(t, func() bool { return dynamicTS2(t, stack) == nil }, 5*time.Second, 20*time.Millisecond,
			"the dynamic talkgroup must unlink after the timeout")
	})
}

func TestDynamicTalkgroupWithNoTimeoutStays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		cur := routingStack(t, stack, config.Routing{DynamicTimeout: 0})
		c := oneHotspot(t, stack, 3100)

		tgCall(t, c, 3100, 701)
		require.Eventually(t, func() bool { return dynamicTS2(t, stack) != nil }, 5*time.Second, 20*time.Millisecond)
		time.Sleep(1500 * time.Millisecond)
		require.NotNil(t, dynamicTS2(t, stack), "a timeout of 0 never unlinks")

		setRouting(cur, config.Routing{DynamicTimeout: 500 * time.Millisecond})
		require.Eventually(t, func() bool { return dynamicTS2(t, stack) == nil }, 5*time.Second, 20*time.Millisecond,
			"a timeout that Setup sets while the hub runs applies to the links that exist")
	})
}

func TestDisabledDynamicLinksLinkNothing(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{DisableDynamicLinks: true})
		c := oneHotspot(t, stack, 3100, 91)
		require.NoError(t, c.SendOptions("DIAL=91;"))

		tgCall(t, c, 3100, 801)
		time.Sleep(time.Second)
		assert.Nil(t, dynamicTS2(t, stack), "with dynamic links off neither a call nor a default dial links a talkgroup")
	})
}

// TestRoutingSettingsReachEveryReplica turns dynamic links off in Setup on
// one replica. A hotspot on the other replica then links nothing.
func TestRoutingSettingsReachEveryReplica(t *testing.T) { //nolint:paralleltest // FreeUDPPort frees its port before the server binds it, so a parallel test could take it
	for _, be := range liveBackends() { //nolint:paralleltest // as the test above
		t.Run(be.Name, func(t *testing.T) { //nolint:paralleltest // as the test above
			l := testutils.SetupLiveStack(t, be)
			b := l.SpawnLiveReplica(t, testutils.LiveOptions{})
			l.SeedUser(t, 1000001, "N0CALL")
			l.SeedTalkgroup(t, 3100, "TG")
			l.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)

			code, _, raw := putSetting(t, l.First(), map[string]any{"dmr.routing.disable-dynamic-links": true})
			require.Equal(t, http.StatusOK, code, raw)

			c := testutils.NewMMDVMClient(100001, "", testPassword)
			require.NoError(t, c.Connect(b.Addr(t, manager.MMDVM)))
			t.Cleanup(c.Close)
			require.NoError(t, c.WaitReady(handshakeWait))
			time.Sleep(settleDuration)
			tgCall(t, c, 3100, 901)
			time.Sleep(time.Second)
			assert.Nil(t, dynamicTS2(t, l.IntegrationStack), "the other replica must take the setting")
		})
	}
}

// listenerPair logs in the hotspot 100001 of N0CALL and the listener
// 100002, which both carry TG 3100 on TS1.
func listenerPair(t *testing.T, stack *testutils.IntegrationStack) (*testutils.MMDVMClient, *testutils.MMDVMClient) {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedTalkgroup(t, 3100, "TG")
	for _, id := range []uint{100001, 100002} {
		stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, id, 3100)
	}
	stack.StartServers(t)
	out := make([]*testutils.MMDVMClient, 0, 2)
	for _, id := range []uint32{100001, 100002} {
		c := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		t.Cleanup(c.Close)
		require.NoError(t, c.WaitReady(handshakeWait))
		out = append(out, c)
	}
	time.Sleep(settleDuration)
	return out[0], out[1]
}

// heardFrom has talker key up TG 3100 on TS1 as radio src, and reports
// whether listener got the call.
func heardFrom(t *testing.T, talker, listener *testutils.MMDVMClient, src, stream uint) bool {
	t.Helper()
	require.NoError(t, talker.SendDMRD(makeGroupVoicePacket(src, 3100, stream, false)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, talker.SendDMRD(makeGroupVoiceTermPacket(src, 3100, stream, false)))
	for _, p := range listener.Drain(drainWait) {
		if p.StreamID == stream {
			return true
		}
	}
	return false
}

const (
	radioIDListed   = uint(3191868)
	radioIDUnlisted = uint(1000002)
	addedRadioID    = uint(1000003)
)

func TestTransmitAnyTakesEveryRadio(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		a, b := listenerPair(t, stack)
		assert.True(t, heardFrom(t, a, b, radioIDUnlisted, 1101), "by default any radio may transmit")
	})
}

func TestTransmitRadioIDTakesOnlyListedRadios(t *testing.T) {
	t.Parallel()
	_, listed := userdb.Get(radioIDListed)
	require.True(t, listed, "the RadioID list must have %d", radioIDListed)
	_, unlisted := userdb.Get(radioIDUnlisted)
	require.False(t, unlisted, "the RadioID list must not have %d", radioIDUnlisted)
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{Transmit: config.TransmitRadioID})
		a, b := listenerPair(t, stack)
		assert.True(t, heardFrom(t, a, b, radioIDListed, 1201), "a radio ID that RadioID lists may transmit")
		assert.False(t, heardFrom(t, a, b, radioIDUnlisted, 1202), "a radio ID that RadioID does not list may not")
	})
}

func TestTransmitMembersTakesAccountsAndTheirAddedIDs(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{Transmit: config.TransmitMembers})
		a, b := listenerPair(t, stack)
		require.NoError(t, stack.DB.Create(&models.UserRadioID{UserID: 1000001, RadioID: addedRadioID, VerifiedAt: time.Now()}).Error)
		assert.True(t, heardFrom(t, a, b, 1000001, 1301), "the DMR ID of an account may transmit")
		assert.True(t, heardFrom(t, a, b, addedRadioID, 1302), "a radio ID that a user added may transmit")
		assert.False(t, heardFrom(t, a, b, radioIDListed, 1303), "a radio ID with no account may not, even when RadioID lists it")
	})
}

// longCall has talker send a voice call of n bursts, one each 60 ms, as
// radio 1000001 on TG 3100, and returns the bursts of it that listener got.
func longCall(t *testing.T, talker, listener *testutils.MMDVMClient, stream uint, n int) int {
	t.Helper()
	for i := range n {
		p := makeGroupVoicePacket(1000001, 3100, stream, false)
		p.Seq = uint(i)
		require.NoError(t, talker.SendDMRD(p))
		time.Sleep(60 * time.Millisecond)
	}
	term := makeGroupVoiceTermPacket(1000001, 3100, stream, false)
	term.Seq = uint(n)
	require.NoError(t, talker.SendDMRD(term))
	got := 0
	for _, p := range listener.Drain(drainWait) {
		if p.StreamID == stream {
			got++
		}
	}
	return got
}

func TestMaxTransmissionCutsALongCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{MaxTransmission: 500 * time.Millisecond})
		a, b := listenerPair(t, stack)
		got := longCall(t, a, b, 1401, 25)
		assert.Positive(t, got, "the start of a long call goes through")
		assert.Less(t, got, 15, "DMRHub drops a call after the longest transmission")
		assert.Equal(t, 4, longCall(t, a, b, 1402, 3), "a short call after it goes through whole")
	})
}

func TestNoMaxTransmissionKeepsALongCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		a, b := listenerPair(t, stack)
		assert.Equal(t, 26, longCall(t, a, b, 1501, 25), "with no limit a long call goes through whole")
	})
}

// talkgroupOf returns talkgroup 3999, and whether it exists.
func talkgroupOf(t *testing.T, stack *testutils.IntegrationStack) (models.Talkgroup, bool) {
	t.Helper()
	var tgs []models.Talkgroup
	require.NoError(t, stack.DB.Where("id = ?", 3999).Find(&tgs).Error)
	if len(tgs) == 0 {
		return models.Talkgroup{}, false
	}
	return tgs[0], true
}

func TestUnknownTalkgroupIsDroppedByDefault(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		c := oneHotspot(t, stack)
		tgCall(t, c, 3999, 1601)
		time.Sleep(time.Second)
		_, ok := talkgroupOf(t, stack)
		assert.False(t, ok, "by default a call to an unknown talkgroup makes no talkgroup")
	})
}

func TestAutoCreateMakesTheTalkgroupOfACall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{AutoCreate: true})
		stack.SeedUser(t, 1000001, "N0CALL")
		for _, id := range []uint{100001, 100002} {
			stack.SeedMMDVMRepeater(t, id, 1000001, testPassword)
		}
		stack.StartServers(t)
		cs := make([]*testutils.MMDVMClient, 0, 2)
		for _, id := range []uint32{100001, 100002} {
			c := testutils.NewMMDVMClient(id, "", testPassword)
			require.NoError(t, c.Connect(stack.MMDVMAddr))
			t.Cleanup(c.Close)
			require.NoError(t, c.WaitReady(handshakeWait))
			cs = append(cs, c)
		}
		time.Sleep(settleDuration)

		tgCall(t, cs[0], 3999, 1701)
		require.Eventually(t, func() bool { _, ok := talkgroupOf(t, stack); return ok }, 5*time.Second, 20*time.Millisecond,
			"a call to an unknown talkgroup makes it")
		tg, _ := talkgroupOf(t, stack)
		assert.True(t, tg.AutoCreated)
		require.Eventually(t, func() bool { d := dynamicTS2(t, stack); return d != nil && *d == 3999 }, 5*time.Second, 20*time.Millisecond,
			"the call links the new talkgroup as the dynamic talkgroup of its timeslot")
		cs[0].Drain(100 * time.Millisecond)

		tgCall(t, cs[1], 3999, 1702)
		heard := false
		for _, p := range cs[0].Drain(drainWait) {
			heard = heard || p.StreamID == 1702
		}
		assert.True(t, heard, "a call on the new talkgroup reaches the hotspot that linked it")
	})
}

func TestEphemeralTalkgroupLivesWhileItHasCalls(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: 1500 * time.Millisecond})
		c := oneHotspot(t, stack)
		rules := stack.PubSub.Subscribe("hub:rules:changed")
		t.Cleanup(func() { _ = rules.Close() })

		for i := range uint(6) {
			tgCall(t, c, 3999, 1800+i)
			time.Sleep(500 * time.Millisecond)
			_, ok := talkgroupOf(t, stack)
			require.True(t, ok, "each call restarts the TTL of the talkgroup")
		}
		require.Eventually(t, func() bool { _, ok := talkgroupOf(t, stack); return !ok }, 10*time.Second, 50*time.Millisecond,
			"the talkgroup goes after its TTL with no call")
		assert.Nil(t, dynamicTS2(t, stack), "the deleted talkgroup is no longer linked")
		select {
		case msg := <-rules.Channel():
			assert.True(t, strings.HasSuffix(string(msg), ":3999"), "notice %q", msg)
		case <-time.After(2 * time.Second):
			assert.Fail(t, "the delete of the talkgroup did not announce a rules change")
		}
	})
}

func TestEditedEphemeralTalkgroupStays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: 500 * time.Millisecond})
		c := oneHotspot(t, stack)
		tgCall(t, c, 3999, 1901)
		require.Eventually(t, func() bool { _, ok := talkgroupOf(t, stack); return ok }, 5*time.Second, 20*time.Millisecond)
		require.NoError(t, stack.DB.Model(&models.Talkgroup{}).Where("id = ?", 3999).Update("auto_created", false).Error)
		stack.Hub.TalkgroupChanged(3999)
		time.Sleep(2 * time.Second)
		_, ok := talkgroupOf(t, stack)
		assert.True(t, ok, "a talkgroup that an admin edited is no longer ephemeral")
	})
}
