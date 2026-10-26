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

func (f *fakePlugins) Deliver(id uint, p models.Packet) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.bursts[id] = append(f.bursts[id], p)
	return nil
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

func TestAHeardRadioIsToldToThePlugins(t *testing.T) {
	t.Parallel()
	h, database, ps := makeTestHubWithPubSub(t)
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, 100003, 1000002)
	require.NoError(t, database.Model(&models.Repeater{}).Where("id = ?", 100003).Update("color_code", 7).Error)
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), 100003)
	h.SetPlugins(newFakePlugins(nil))
	sub := ps.Subscribe(plugins.HeardTopic)
	t.Cleanup(func() { _ = sub.Close() })
	p := dataHeader(1000002, 1, 100003, 1)
	p.Slot = true
	h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	select {
	case b := <-sub.Channel():
		radio, repeater, slot, cc, ok := plugins.DecodeHeard(b)
		require.True(t, ok)
		assert.Equal(t, []uint{1000002, 100003}, []uint{radio, repeater})
		assert.Equal(t, []uint8{2, 7}, []uint8{slot, cc})
	case <-time.After(2 * time.Second):
		t.Fatal("the plugins were not told where the radio was heard")
	}
}

func TestAPluginsCallStaysOnThisHub(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	f := newFakePlugins(map[uint]uint{9990: parrotPlugin})
	h.SetPlugins(f)
	owner := models.User{ID: 2000001, Callsign: "PEEROWNER", Username: "PEEROWNER", Approved: true}
	require.NoError(t, database.Create(&owner).Error)
	require.NoError(t, database.Create(&models.Peer{ID: 200, OwnerID: owner.ID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: 200, Direction: false, SubjectIDMin: 1, SubjectIDMax: 16777215}).Error)
	peer := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
	t.Cleanup(func() { h.UnregisterServer("peer-srv") })

	h.RoutePacket(context.Background(), dataHeader(1000002, 1000001, 100003, 1), models.RepeaterTypeMMDVM)
	select {
	case <-peer.Packets:
	case <-time.After(2 * time.Second):
		t.Fatal("the peer does not get the private calls of a radio, so this test proves nothing")
	}
	for {
		select {
		case <-srv.Packets:
			continue
		case <-time.After(200 * time.Millisecond):
		}
		break
	}

	reply := makeVoicePacket(1000002, 7006, false, true)
	reply.Src, reply.Repeater = 9990, 0
	h.RoutePacket(context.Background(), reply, plugins.SourceName(parrotPlugin))
	expectAt(t, srv, 100003, "the radio's repeater")
	select {
	case rp := <-peer.Packets:
		t.Fatalf("the peer got the plugin's call: %v", rp.Packet)
	case <-time.After(500 * time.Millisecond):
	}
	assert.Empty(t, f.got(parrotPlugin))
}

// pluginPeerHub returns pluginHub with a peer whose egress takes every
// source.
func pluginPeerHub(t *testing.T) (*hub.Hub, *hub.ServerHandle, *hub.ServerHandle) {
	t.Helper()
	h, database, srv := lastSeenHub(t)
	seedTalkgroup(t, database, weatherTG, "Weather")
	h.SetPlugins(newFakePlugins(map[uint]uint{9990: parrotPlugin, weatherTG: weatherPlugin, 310009: weatherPlugin}))
	owner := models.User{ID: 2000001, Callsign: "PEEROWNER", Username: "PEEROWNER", Approved: true}
	require.NoError(t, database.Create(&owner).Error)
	require.NoError(t, database.Create(&models.Peer{ID: 200, OwnerID: owner.ID, Egress: true}).Error)
	require.NoError(t, database.Create(&models.PeerRule{PeerID: 200, Direction: false, SubjectIDMin: 1, SubjectIDMax: 16777215}).Error)
	peer := h.RegisterServer(hub.ServerConfig{Name: "peer-srv", Role: hub.RolePeer})
	t.Cleanup(func() { h.UnregisterServer("peer-srv") })
	return h, srv, peer
}

func TestAPluginsGroupCallReachesThePeers(t *testing.T) {
	t.Parallel()
	h, _, peer := pluginPeerHub(t)
	p := makeVoicePacket(weatherTG, 7007, true, false)
	p.Src, p.Repeater = 310009, 0
	h.RoutePacket(context.Background(), p, plugins.SourceName(weatherPlugin))
	select {
	case rp := <-peer.Packets:
		assert.Equal(t, uint(310009), rp.Packet.Src)
	case <-time.After(2 * time.Second):
		t.Fatal("the peer did not get the plugin's group call")
	}
}

func TestAPluginsPrivateCallToARadioNotHereReachesThePeers(t *testing.T) {
	t.Parallel()
	h, srv, peer := pluginPeerHub(t)
	p := makeVoicePacket(3999999, 7008, false, true)
	p.Src, p.Repeater = 9990, 0
	h.RoutePacket(context.Background(), p, plugins.SourceName(parrotPlugin))
	select {
	case rp := <-peer.Packets:
		assert.Equal(t, uint(3999999), rp.Packet.Dst)
	case <-time.After(2 * time.Second):
		t.Fatal("the peer did not get a private call the hub cannot deliver")
	}
	expectNone(t, srv, "the radio is not here")
}
