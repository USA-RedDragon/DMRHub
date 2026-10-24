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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakePlugins struct {
	mu     sync.Mutex
	owned  map[uint]uint
	bursts map[uint][]models.Packet
}

func newFakePlugins(owned map[uint]uint) *fakePlugins {
	return &fakePlugins{owned: owned, bursts: map[uint][]models.Packet{}}
}

func (f *fakePlugins) Owner(id uint) (uint, bool) {
	p, ok := f.owned[id]
	return p, ok
}

func (f *fakePlugins) Deliver(id uint, p models.Packet) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bursts[id] = append(f.bursts[id], p)
}

func (f *fakePlugins) got(id uint) []models.Packet {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]models.Packet(nil), f.bursts[id]...)
}

const (
	parrotPlugin  = uint(1)
	weatherPlugin = uint(2)
	weatherTG     = uint(310001)
)

func pluginHub(t *testing.T) (*hub.Hub, *fakePlugins, *hub.ServerHandle) {
	t.Helper()
	h, database, srv := lastSeenHub(t)
	seedTalkgroup(t, database, weatherTG, "Weather")
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100004).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: weatherTG}))
	h.ReloadRepeater(context.Background(), 100004)
	time.Sleep(100 * time.Millisecond)
	f := newFakePlugins(map[uint]uint{9990: parrotPlugin, weatherTG: weatherPlugin, 310009: weatherPlugin})
	h.SetPlugins(f)
	return h, f, srv
}

func TestAPrivateCallToAPluginGoesOnlyToThePlugin(t *testing.T) {
	t.Parallel()
	h, f, srv := pluginHub(t)
	for i, p := range []models.Packet{makeVoicePacket(9990, 7001, false, true), makeVoiceTermPacket(9990, 7001, false, true)} {
		p.Seq = uint(i)
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	require.Eventually(t, func() bool { return len(f.got(parrotPlugin)) == 2 }, 2*time.Second, 10*time.Millisecond)
	assert.Equal(t, uint(9990), f.got(parrotPlugin)[0].Dst)
	expectNone(t, srv, "the plugin's ID replaces the built-in parrot, which plays nothing back")
}

func TestAGroupCallToAPluginsTalkgroupAlsoReachesItsSubscribers(t *testing.T) {
	t.Parallel()
	h, f, srv := pluginHub(t)
	p := makeVoicePacket(weatherTG, 7002, true, false)
	p.Repeater = 100003
	h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100004, "the subscribers of the talkgroup")
	require.Eventually(t, func() bool { return len(f.got(weatherPlugin)) == 1 }, 2*time.Second, 10*time.Millisecond)
}

func TestAGroupCallToAPluginIDThatIsNoTalkgroupGoesOnlyToThePlugin(t *testing.T) {
	t.Parallel()
	h, f, srv := pluginHub(t)
	h.RoutePacket(context.Background(), makeVoicePacket(310009, 7003, true, false), models.RepeaterTypeMMDVM)
	require.Eventually(t, func() bool { return len(f.got(weatherPlugin)) == 1 }, 2*time.Second, 10*time.Millisecond)
	expectNone(t, srv, "no talkgroup, no subscribers")
}

func TestAPluginsPrivateCallReachesTheRadioWhereItIsHeard(t *testing.T) {
	t.Parallel()
	h, f, srv := pluginHub(t)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	time.Sleep(200 * time.Millisecond)

	reply := makeVoicePacket(1000002, 7004, false, true)
	reply.Src, reply.Repeater = 9990, 0
	h.RoutePacket(context.Background(), reply, plugins.SourceName(parrotPlugin))
	expectAt(t, srv, 100003, "the radio's last repeater")
	assert.Empty(t, f.got(parrotPlugin))

	h.RoutePacket(context.Background(), dataHeader(1000001, 9990, 100004, 9), models.RepeaterTypeMMDVM)
	require.Eventually(t, func() bool { return len(f.got(parrotPlugin)) == 1 }, 2*time.Second, 10*time.Millisecond,
		"the plugin's own call did not make its ID a radio heard on no repeater")
}

func TestAPluginDoesNotHearItsOwnGroupCall(t *testing.T) {
	t.Parallel()
	h, f, srv := pluginHub(t)
	p := makeVoicePacket(weatherTG, 7005, true, false)
	p.Src, p.Repeater = 310009, 0
	h.RoutePacket(context.Background(), p, plugins.SourceName(weatherPlugin))
	expectAt(t, srv, 100004, "the subscribers of the talkgroup")
	assert.Empty(t, f.got(weatherPlugin), "no echo to the plugin that sent it")
}
