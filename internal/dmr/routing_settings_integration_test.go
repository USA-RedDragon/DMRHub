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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers/manager"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
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

func dynamicTS2(t *testing.T, stack *testutils.IntegrationStack, id uint) *uint {
	t.Helper()
	rpt, err := models.FindRepeaterByID(stack.DB, id)
	require.NoError(t, err)
	return rpt.TS2DynamicTalkgroupID
}

func tgCall(t *testing.T, c *testutils.MMDVMClient, src, dst, stream uint) {
	t.Helper()
	require.NoError(t, c.SendDMRD(makeGroupVoicePacket(src, dst, stream, true)))
	time.Sleep(50 * time.Millisecond)
	require.NoError(t, c.SendDMRD(makeGroupVoiceTermPacket(src, dst, stream, true)))
}

func TestDynamicTalkgroupTimesOut(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{DynamicTimeout: 600 * time.Millisecond})
		c := oneHotspot(t, stack, 3100)

		tgCall(t, c, 1000001, 3100, 501)
		require.Eventually(t, func() bool { return dynamicTS2(t, stack, 100001) != nil }, 5*time.Second, 20*time.Millisecond,
			"a call on a talkgroup links it")
		start := time.Now()
		require.Eventually(t, func() bool { return dynamicTS2(t, stack, 100001) == nil }, 5*time.Second, 20*time.Millisecond,
			"the dynamic talkgroup must unlink after the timeout")
		assert.GreaterOrEqual(t, time.Since(start), 300*time.Millisecond, "the link must last about the timeout")
	})
}

func TestDynamicTalkgroupTimeoutRestartsAtEachCall(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{DynamicTimeout: 1500 * time.Millisecond})
		c := oneHotspot(t, stack, 3100)

		for i := range uint(4) {
			tgCall(t, c, 1000001, 3100, 600+i)
			time.Sleep(700 * time.Millisecond)
			require.NotNil(t, dynamicTS2(t, stack, 100001), "a call on the dynamic talkgroup restarts its timeout")
		}
	})
}

func TestDynamicTalkgroupWithNoTimeoutStays(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		cur := routingStack(t, stack, config.Routing{DynamicTimeout: 0})
		c := oneHotspot(t, stack, 3100)

		tgCall(t, c, 1000001, 3100, 701)
		require.Eventually(t, func() bool { return dynamicTS2(t, stack, 100001) != nil }, 5*time.Second, 20*time.Millisecond)
		time.Sleep(1500 * time.Millisecond)
		require.NotNil(t, dynamicTS2(t, stack, 100001), "a timeout of 0 never unlinks")

		setRouting(cur, config.Routing{DynamicTimeout: 500 * time.Millisecond})
		require.Eventually(t, func() bool { return dynamicTS2(t, stack, 100001) == nil }, 5*time.Second, 20*time.Millisecond,
			"a timeout that Setup sets while the hub runs applies to the links that exist")
	})
}

func TestDisabledDynamicLinksLinkNothing(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		routingStack(t, stack, config.Routing{DisableDynamicLinks: true})
		c := oneHotspot(t, stack, 3100, 91)
		require.NoError(t, c.SendOptions("DIAL=91;"))

		tgCall(t, c, 1000001, 3100, 801)
		time.Sleep(time.Second)
		assert.Nil(t, dynamicTS2(t, stack, 100001), "with dynamic links off neither a call nor a default dial links a talkgroup")
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
			tgCall(t, c, 1000001, 3100, 901)
			time.Sleep(time.Second)
			assert.Nil(t, dynamicTS2(t, l.IntegrationStack, 100001), "the other replica must take the setting")
		})
	}
}
