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
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
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

func forAllBackendsOnTestClock(t *testing.T, f func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake)) {
	t.Helper()
	for _, be := range allBackends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			fake := clock.NewFake(time.Now())
			be.HubOptions = []hub.Option{hub.WithClock(fake)}
			f(t, testutils.SetupIntegrationStack(t, be), fake)
		})
	}
}

func hotspots(t *testing.T, stack *testutils.IntegrationStack, ids []uint32, talkgroups ...uint) []*testutils.MMDVMClient {
	t.Helper()
	stack.SeedUser(t, 1000001, "N0CALL")
	for _, tg := range talkgroups {
		stack.SeedTalkgroup(t, tg, "TG")
	}
	for _, id := range ids {
		stack.SeedMMDVMRepeater(t, uint(id), 1000001, testPassword)
	}
	stack.StartServers(t)
	out := make([]*testutils.MMDVMClient, 0, len(ids))
	for _, id := range ids {
		c := testutils.NewMMDVMClient(id, "", testPassword)
		require.NoError(t, c.Connect(stack.MMDVMAddr))
		t.Cleanup(c.Close)
		require.NoError(t, c.WaitReady(handshakeWait))
		out = append(out, c)
	}
	return out
}

func oneHotspot(t *testing.T, stack *testutils.IntegrationStack, talkgroups ...uint) *testutils.MMDVMClient {
	t.Helper()
	return hotspots(t, stack, []uint32{100001}, talkgroups...)[0]
}

// dynamicTS2 returns the dynamic talkgroup of TS2 of the hotspot 100001.
func dynamicTS2(t *testing.T, stack *testutils.IntegrationStack) *uint {
	t.Helper()
	rpt, err := models.FindRepeaterByID(stack.DB, 100001)
	require.NoError(t, err)
	return rpt.TS2DynamicTalkgroupID
}

func dynamicLinks(t *testing.T, stack *testutils.IntegrationStack, repeaterID uint) <-chan uint {
	t.Helper()
	ch := make(chan uint, 64)
	t.Cleanup(stack.Hub.OnRepeaterChange(func(id uint) {
		if id != repeaterID {
			return
		}
		rpt, err := models.FindRepeaterByID(stack.DB, id)
		if err != nil {
			return
		}
		var tg uint
		if rpt.TS2DynamicTalkgroupID != nil {
			tg = *rpt.TS2DynamicTalkgroupID
		}
		ch <- tg
	}))
	return ch
}

func waitLinked(t *testing.T, links <-chan uint, tg uint, msg string) {
	t.Helper()
	got := testutils.Collect(links, deliverWait, func(got []uint) bool { return got[len(got)-1] == tg })
	require.True(t, len(got) > 0 && got[len(got)-1] == tg, "%s: the dynamic talkgroups were %v", msg, got)
}

func talkgroupChanges(t *testing.T, stack *testutils.IntegrationStack) <-chan uint {
	t.Helper()
	ch := make(chan uint, 64)
	t.Cleanup(stack.Hub.OnTalkgroupChange(func(id uint) { ch <- id }))
	return ch
}

func waitChanged(t *testing.T, changes <-chan uint, tg uint, msg string) {
	t.Helper()
	got := testutils.Collect(changes, deliverWait, func(got []uint) bool { return got[len(got)-1] == tg })
	require.True(t, len(got) > 0 && got[len(got)-1] == tg, msg)
}

// tgCall has c send a short call of radio 1000001 to dst on TS2.
func tgCall(t *testing.T, c *testutils.MMDVMClient, dst, stream uint) {
	t.Helper()
	require.NoError(t, c.SendDMRD(makeGroupVoicePacket(1000001, dst, stream, true)))
	require.NoError(t, c.SendDMRD(makeGroupVoiceTermPacket(1000001, dst, stream, true)))
}

// settleCalls waits until the hub routed every burst of the calls sent and
// the call tracker stored them, so that a step of the test clock comes
// after them.
func settleCalls(stack *testutils.IntegrationStack) {
	stack.Hub.WaitForCalls()
	stack.CallTracker.Flush()
}

func streamEnds(stream uint) func([]models.Packet) bool {
	return func(got []models.Packet) bool {
		p := got[len(got)-1]
		return p.StreamID == stream && isTerminator(p)
	}
}

func countStream(got []models.Packet, stream uint) int {
	n := 0
	for _, p := range got {
		if p.StreamID == stream {
			n++
		}
	}
	return n
}

func TestDynamicTalkgroupTimesOut(t *testing.T) {
	t.Parallel()
	forAllBackendsOnTestClock(t, func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake) {
		const timeout = 600 * time.Millisecond
		routingStack(t, stack, config.Routing{DynamicTimeout: timeout})
		c := oneHotspot(t, stack, 3100)
		links := dynamicLinks(t, stack, 100001)

		tgCall(t, c, 3100, 501)
		waitLinked(t, links, 3100, "a call on a talkgroup links it")
		settleCalls(stack)
		fake.Advance(timeout + time.Second)
		waitLinked(t, links, 0, "the dynamic talkgroup must unlink after the timeout")
	})
}

func TestDynamicTalkgroupWithNoTimeoutStays(t *testing.T) {
	t.Parallel()
	forAllBackendsOnTestClock(t, func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake) {
		cur := routingStack(t, stack, config.Routing{DynamicTimeout: 0})
		c := oneHotspot(t, stack, 3100)
		links := dynamicLinks(t, stack, 100001)

		tgCall(t, c, 3100, 701)
		waitLinked(t, links, 3100, "a call on a talkgroup links it")
		settleCalls(stack)
		fake.Advance(3 * time.Second)
		time.Sleep(drainWait)
		require.NotNil(t, dynamicTS2(t, stack), "a timeout of 0 never unlinks")

		setRouting(cur, config.Routing{DynamicTimeout: 500 * time.Millisecond})
		fake.Advance(time.Second + 500*time.Millisecond)
		waitLinked(t, links, 0, "a timeout that Setup sets while the hub runs applies to the links that exist")
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
	return out[0], out[1]
}

// heardFrom has talker key up TG 3100 on TS1 as radio src, and then as
// radio allowed, which the transmit setting takes, and reports whether
// listener got the call of src. The hub routes the calls of a slot of a
// repeater in the order they came, so the call of src was routed or
// dropped before the call of allowed arrives.
func heardFrom(t *testing.T, talker, listener *testutils.MMDVMClient, src, stream, allowed uint) bool {
	t.Helper()
	probe := stream + 50
	for _, c := range []struct{ src, stream uint }{{src, stream}, {allowed, probe}} {
		require.NoError(t, talker.SendDMRD(makeGroupVoicePacket(c.src, 3100, c.stream, false)))
		require.NoError(t, talker.SendDMRD(makeGroupVoiceTermPacket(c.src, 3100, c.stream, false)))
	}
	got := testutils.Collect(listener.ReceivedPackets(), deliverWait, streamEnds(probe))
	require.Positive(t, countStream(got, probe), "the call of radio %d did not arrive", allowed)
	return countStream(got, stream) > 0
}

const (
	radioIDListed   = uint(3002068)
	radioIDUnlisted = uint(1000002)
	addedRadioID    = uint(1000003)
)

func TestTransmitAnyTakesEveryRadio(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		a, b := listenerPair(t, stack)
		assert.True(t, heardFrom(t, a, b, radioIDUnlisted, 1101, 1000001), "by default any radio may transmit")
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
		assert.True(t, heardFrom(t, a, b, radioIDListed, 1201, radioIDListed), "a radio ID that RadioID lists may transmit")
		assert.False(t, heardFrom(t, a, b, radioIDUnlisted, 1202, radioIDListed), "a radio ID that RadioID does not list may not")
	})
}

func TestTransmitMembersTakesAccountsAndTheirAddedIDs(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{Transmit: config.TransmitMembers})
		a, b := listenerPair(t, stack)
		require.NoError(t, stack.DB.Create(&models.UserRadioID{UserID: 1000001, RadioID: addedRadioID, VerifiedAt: time.Now()}).Error)
		assert.True(t, heardFrom(t, a, b, 1000001, 1301, 1000001), "the DMR ID of an account may transmit")
		assert.True(t, heardFrom(t, a, b, addedRadioID, 1302, 1000001), "a radio ID that a user added may transmit")
		assert.False(t, heardFrom(t, a, b, radioIDListed, 1303, 1000001), "a radio ID with no account may not, even when RadioID lists it")
	})
}

func sendBursts(t *testing.T, talker *testutils.MMDVMClient, stream uint, first, last int, end bool) {
	t.Helper()
	for i := first; i <= last; i++ {
		p := makeGroupVoicePacket(1000001, 3100, stream, false)
		p.Seq = uint(i)
		require.NoError(t, talker.SendDMRD(p))
	}
	if end {
		term := makeGroupVoiceTermPacket(1000001, 3100, stream, false)
		term.Seq = uint(last + 1)
		require.NoError(t, talker.SendDMRD(term))
	}
}

func TestMaxTransmissionCutsALongCall(t *testing.T) {
	t.Parallel()
	forAllBackendsOnTestClock(t, func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake) {
		const most = 500 * time.Millisecond
		routingStack(t, stack, config.Routing{MaxTransmission: most})
		a, b := listenerPair(t, stack)

		sendBursts(t, a, 1401, 0, 2, false)
		start := testutils.Collect(b.ReceivedPackets(), deliverWait, func(got []models.Packet) bool { return countStream(got, 1401) >= 3 })
		assert.Equal(t, 3, countStream(start, 1401), "the start of a long call goes through")

		fake.Advance(most + time.Millisecond)
		sendBursts(t, a, 1401, 3, 24, true)
		sendBursts(t, a, 1402, 0, 2, true)
		rest := testutils.Collect(b.ReceivedPackets(), deliverWait, streamEnds(1402))
		assert.Zero(t, countStream(rest, 1401), "DMRHub drops a call after the longest transmission")
		assert.Equal(t, 4, countStream(rest, 1402), "a short call after it goes through whole")
	})
}

func TestNoMaxTransmissionKeepsALongCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		a, b := listenerPair(t, stack)
		sendBursts(t, a, 1501, 0, 24, true)
		got := testutils.Collect(b.ReceivedPackets(), deliverWait, streamEnds(1501))
		assert.Equal(t, 26, countStream(got, 1501), "with no limit a long call goes through whole")
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

// TestUnknownTalkgroupIsDroppedByDefault sends a call to the unknown TG
// 3999 and then one to TG 3100. The hub routes the calls of a slot in the
// order they came, so once the second call links TG 3100 the first call
// was routed.
func TestUnknownTalkgroupIsDroppedByDefault(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{})
		c := oneHotspot(t, stack, 3100)
		links := dynamicLinks(t, stack, 100001)
		tgCall(t, c, 3999, 1601)
		tgCall(t, c, 3100, 1602)
		waitLinked(t, links, 3100, "the call after it links its talkgroup")
		_, ok := talkgroupOf(t, stack)
		assert.False(t, ok, "by default a call to an unknown talkgroup makes no talkgroup")
	})
}

func TestAutoCreateMakesTheTalkgroupOfACall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{AutoCreate: true})
		changes := talkgroupChanges(t, stack)
		cs := hotspots(t, stack, []uint32{100001, 100002})
		links := dynamicLinks(t, stack, 100001)

		tgCall(t, cs[0], 3999, 1701)
		waitChanged(t, changes, 3999, "a call to an unknown talkgroup makes it")
		tg, ok := talkgroupOf(t, stack)
		require.True(t, ok)
		assert.True(t, tg.AutoCreated)
		waitLinked(t, links, 3999, "the call links the new talkgroup as the dynamic talkgroup of its timeslot")
		cs[0].Drain(100 * time.Millisecond)

		tgCall(t, cs[1], 3999, 1702)
		got := testutils.Collect(cs[0].ReceivedPackets(), deliverWait, streamEnds(1702))
		assert.Positive(t, countStream(got, 1702), "a call on the new talkgroup reaches the hotspot that linked it")
	})
}

func TestEphemeralTalkgroupLivesWhileItHasCalls(t *testing.T) {
	t.Parallel()
	forAllBackendsOnTestClock(t, func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake) {
		const ttl = 1500 * time.Millisecond
		routingStack(t, stack, config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: ttl})
		changes := talkgroupChanges(t, stack)
		cs := hotspots(t, stack, []uint32{100001, 100002})
		c, listener := cs[0], cs[1]
		rules := stack.PubSub.Subscribe("hub:rules:changed")
		t.Cleanup(func() { _ = rules.Close() })
		links, heard := dynamicLinks(t, stack, 100001), dynamicLinks(t, stack, 100002)

		tgCall(t, c, 3999, 1799)
		waitChanged(t, changes, 3999, "a call to an unknown talkgroup makes it")
		waitLinked(t, links, 3999, "the call links the new talkgroup")
		tgCall(t, listener, 3999, 1798)
		waitLinked(t, heard, 3999, "the call of the listener links the new talkgroup")

		for i := range uint(6) {
			tgCall(t, c, 3999, 1800+i)
			got := testutils.Collect(listener.ReceivedPackets(), deliverWait, streamEnds(1800+i))
			require.Positive(t, countStream(got, 1800+i), "call %d reaches the listener", i)
			settleCalls(stack)
			fake.Advance(ttl / 3)
			_, ok := talkgroupOf(t, stack)
			require.True(t, ok, "each call restarts the TTL of the talkgroup")
		}
		settleCalls(stack)
		fake.Advance(ttl + time.Second)
		waitChanged(t, changes, 3999, "the talkgroup goes after its TTL with no call")
		_, ok := talkgroupOf(t, stack)
		assert.False(t, ok, "the talkgroup goes after its TTL with no call")
		assert.Nil(t, dynamicTS2(t, stack), "the deleted talkgroup is no longer linked")
		want := fmt.Sprintf(":%d", 3999)
		notices := testutils.Collect(rules.Channel(), deliverWait, func(got [][]byte) bool { return strings.HasSuffix(string(got[len(got)-1]), want) })
		require.NotEmpty(t, notices, "the delete of the talkgroup did not announce a rules change")
		assert.True(t, strings.HasSuffix(string(notices[len(notices)-1]), want), "notices %q", notices)
	})
}

// TestEditedEphemeralTalkgroupStays edits TG 3999, which the hub made, and
// then makes TG 3998. Once the TTL passed, the sweep that deletes TG 3998
// must keep TG 3999.
func TestEditedEphemeralTalkgroupStays(t *testing.T) {
	t.Parallel()
	forAllBackendsOnTestClock(t, func(t *testing.T, stack *testutils.IntegrationStack, fake *clock.Fake) {
		const ttl = 500 * time.Millisecond
		routingStack(t, stack, config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: ttl})
		changes := talkgroupChanges(t, stack)
		c := oneHotspot(t, stack)
		links := dynamicLinks(t, stack, 100001)
		tgCall(t, c, 3999, 1901)
		waitChanged(t, changes, 3999, "a call to an unknown talkgroup makes it")
		require.NoError(t, stack.DB.Model(&models.Talkgroup{}).Where("id = ?", 3999).Update("auto_created", false).Error)
		stack.Hub.TalkgroupChanged(3999)
		waitChanged(t, changes, 3999, "the hub takes the edit")
		tgCall(t, c, 3998, 1902)
		waitChanged(t, changes, 3998, "a call to another unknown talkgroup makes it")
		waitLinked(t, links, 3998, "the call links the talkgroup it made")

		settleCalls(stack)
		fake.Advance(ttl + time.Second)
		waitChanged(t, changes, 3998, "the talkgroup that the hub made and nobody edited goes after its TTL")
		_, ok := talkgroupOf(t, stack)
		assert.True(t, ok, "a talkgroup that an admin edited is no longer ephemeral")
	})
}
