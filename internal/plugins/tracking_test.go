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
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	trackedRadio   = uint(3191868)
	untrackedRadio = uint(3140598)
	locatorID      = uint32(5050)
)

// trackingRig returns a rig with a location plugin at 5050 to 5059,
// connected and welcomed, and two radios, one with tracking on.
func trackingRig(t *testing.T, opts ...plugins.ServiceOption) (*rig, pluginv1.PluginService_ConnectClient) {
	t.Helper()
	r := newRig(t, opts...)
	require.NoError(t, r.db.Create(&models.User{ID: trackedRadio, Callsign: "KI5VMF", Username: "tracked", Approved: true}).Error)
	require.NoError(t, r.db.Create(&models.User{ID: untrackedRadio, Callsign: "KD5SMP", Username: "untracked", Approved: true}).Error)
	require.NoError(t, models.SetPositionTracking(r.db, trackedRadio, trackedRadio, true, time.Now().Add(-time.Minute)))
	_, token := r.add("Location", rng(5050, 5059))
	s := r.connect(token)
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetWelcome())
	return r, s
}

func subscribe(t *testing.T, s pluginv1.PluginService_ConnectClient) []*pluginv1.Tracking {
	t.Helper()
	require.NoError(t, s.Send(&pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Subscribe{Subscribe: &pluginv1.Subscribe{Tracking: true}}}))
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetTracked(), "got %v", m)
	return m.GetTracked().GetRadios()
}

func TestAPluginThatSubscribesGetsOnlyTheTrackedRadios(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		_, s := trackingRig(t)
		radios := subscribe(t, s)
		require.Len(t, radios, 1)
		assert.Equal(t, uint32(trackedRadio), radios[0].GetRadio())
		assert.True(t, radios[0].GetOn())
		assert.Equal(t, uint32(600), radios[0].GetIntervalSeconds())
	})
}

func TestAPluginHearsOnlyTrackedRadiosAndOnlyWhenSubscribed(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100001, 2, 7))
		subscribe(t, s)
		require.NoError(t, plugins.PublishHeard(r.ps, untrackedRadio, 100001, 1, 1))
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		m, err := recv(t, s)
		require.NoError(t, err)
		assert.True(t, proto.Equal(&pluginv1.Heard{Radio: uint32(trackedRadio), Slot: 2, ColorCode: 7}, m.GetHeard()), "got %v", m)
	})
}

func nextHeard(t *testing.T, s pluginv1.PluginService_ConnectClient) *pluginv1.Heard {
	t.Helper()
	m, err := recv(t, s)
	require.NoError(t, err)
	require.NotNil(t, m.GetHeard(), "got %v", m)
	return m.GetHeard()
}

func marker(t *testing.T, r *rig, s pluginv1.PluginService_ConnectClient) {
	t.Helper()
	require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100009, 1, 9))
	h := nextHeard(t, s)
	assert.Equal(t, []uint32{1, 9}, []uint32{h.GetSlot(), h.GetColorCode()}, "the marker comes next, with no Heard before it")
}

func TestAPluginHearsANewReplicaOnceWithTheTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		subscribe(t, s)
		at := time.Now().Add(-1500 * time.Millisecond)
		require.NoError(t, plugins.PublishHeardAt(r.ps, trackedRadio, 100002, 2, 7, at))
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		ms := at.UnixMilli()
		h := nextHeard(t, s)
		assert.True(t, proto.Equal(&pluginv1.Heard{Radio: uint32(trackedRadio), Slot: 2, ColorCode: 7, At: &ms}, h), "got %v", h)
		marker(t, r, s)
	})
}

func TestAPluginHearsAnOldReplicaOnceWithNoTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		subscribe(t, s)
		old := []byte{0, 0x30, 0xB4, 0x3C, 0, 0x01, 0x86, 0xA2, 2, 7}
		require.NoError(t, r.ps.Publish(plugins.HeardTopic, old))
		h := nextHeard(t, s)
		assert.True(t, proto.Equal(&pluginv1.Heard{Radio: uint32(trackedRadio), Slot: 2, ColorCode: 7}, h), "got %v", h)
		marker(t, r, s)
	})
}

func TestAPluginHearsEachSightingOfMixedReplicasOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		subscribe(t, s)
		at := time.Now()
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		require.NoError(t, plugins.PublishHeardAt(r.ps, trackedRadio, 100002, 2, 7, at))
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		assert.Nil(t, nextHeard(t, s).At, "the old replica's sighting")
		assert.Equal(t, at.UnixMilli(), nextHeard(t, s).GetAt(), "the new replica's sighting")
		assert.Nil(t, nextHeard(t, s).At, "a second sighting of the old replica")
		marker(t, r, s)
	})
}

func TestACopyThatNeverCameDoesNotHideALaterOldSighting(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fake := clock.NewFake(time.Now())
		r, s := trackingRig(t, plugins.WithClock(fake))
		subscribe(t, s)
		require.NoError(t, plugins.PublishHeardAt(r.ps, trackedRadio, 100002, 2, 7, fake.Now()))
		assert.NotNil(t, nextHeard(t, s).At)
		fake.Advance(plugins.TwinWait)
		require.NoError(t, plugins.PublishHeardAt(r.ps, trackedRadio, 100002, 2, 7, fake.Now()))
		assert.NotNil(t, nextHeard(t, s).At)
		fake.Advance(time.Millisecond)
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		marker(t, r, s)
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		assert.Nil(t, nextHeard(t, s).At, "an old sighting after the copy of the second came")
		marker(t, r, s)
	})
}

func TestAPluginHearsWhenAnOwnerChangesTracking(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		subscribe(t, s)
		require.NoError(t, models.SetPositionTracking(r.db, trackedRadio, trackedRadio, false, time.Now()))
		require.NoError(t, plugins.PublishTracking(r.ps, trackedRadio))
		m, err := recv(t, s)
		require.NoError(t, err)
		require.NotNil(t, m.GetTracking(), "got %v", m)
		assert.Equal(t, uint32(trackedRadio), m.GetTracking().GetRadio())
		assert.False(t, m.GetTracking().GetOn())

		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100001, 1, 1))
		require.NoError(t, models.SetPositionTracking(r.db, untrackedRadio, untrackedRadio, true, time.Now()))
		require.NoError(t, plugins.PublishTracking(r.ps, untrackedRadio))
		m, err = recv(t, s)
		require.NoError(t, err)
		require.NotNil(t, m.GetTracking(), "a radio that is no longer tracked is not heard; got %v", m)
		assert.Equal(t, uint32(untrackedRadio), m.GetTracking().GetRadio())
		assert.True(t, m.GetTracking().GetOn())
	})
}

func position(radio uint, service uint32) *pluginv1.ConnectRequest {
	acc := 12.5
	at := time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC).Unix()
	return &pluginv1.ConnectRequest{Message: &pluginv1.ConnectRequest_Position{Position: &pluginv1.Position{
		Radio: uint32(radio), Service: service, Slot: 2, Format: pluginv1.PositionFormat_POSITION_FORMAT_LRRP, //nolint:gosec // test IDs
		Latitude: 33.1, Longitude: -97.2, AccuracyMeters: &acc, FixTime: &at,
	}}}
}

func TestTheHubKeepsOnlyPositionsOfTrackedRadiosToThePluginsIDs(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r, s := trackingRig(t)
		require.NoError(t, r.db.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 100002, Callsign: "KI5VMF"}, OwnerID: trackedRadio}).Error)
		subscribe(t, s)
		require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
		_, err := recv(t, s)
		require.NoError(t, err)

		require.NoError(t, s.Send(position(untrackedRadio, locatorID)))
		require.NoError(t, s.Send(position(trackedRadio, 9990)))
		require.NoError(t, s.Send(position(trackedRadio, locatorID)))
		bubble.SettleDB(t, r.db, time.Minute)
		got := r.positions()
		require.Len(t, got, 1)
		p := got[0]
		assert.Equal(t, trackedRadio, p.RadioID)
		assert.Equal(t, uint(locatorID), p.ServiceID)
		assert.Equal(t, uint(100002), p.RepeaterID, "the repeater where the radio was heard")
		assert.Equal(t, uint8(2), p.Slot)
		assert.Equal(t, models.PositionFormatLRRP, p.Format)
		require.NotNil(t, p.AccuracyM)
		assert.InDelta(t, 12.5, *p.AccuracyM, 0)
		require.NotNil(t, p.FixTime)
		assert.Equal(t, time.Date(2026, 10, 24, 1, 2, 3, 0, time.UTC), p.FixTime.UTC())
		var stored int64
		require.NoError(t, r.db.Model(&models.Position{}).Count(&stored).Error)
		assert.Equal(t, int64(1), stored)
	})
}

func TestTheHubKeepsPositionsOnlyThroughTheOwnersOrTrustedRepeaters(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		r := newRig(t)
		const witness = uint(3191870)
		require.NoError(t, r.db.Create(&models.User{ID: trackedRadio, Callsign: "KI5VMF", Username: "tracked", Approved: true}).Error)
		require.NoError(t, r.db.Create(&models.User{ID: untrackedRadio, Callsign: "KD5SMP", Username: "untracked", Approved: true}).Error)
		require.NoError(t, r.db.Create(&models.User{ID: witness, Callsign: "N5WIT", Username: "witness", Approved: true}).Error)
		require.NoError(t, models.SetPositionTracking(r.db, trackedRadio, trackedRadio, true, time.Now().Add(-time.Minute)))
		require.NoError(t, models.SetPositionTracking(r.db, witness, witness, true, time.Now().Add(-time.Minute)))
		const own, spoof, trusted, witnessed = uint(100002), uint(100009), uint(100010), uint(100011)
		for _, rpt := range []models.Repeater{
			{RepeaterConfiguration: models.RepeaterConfiguration{ID: own, Callsign: "KI5VMF"}, OwnerID: trackedRadio},
			{RepeaterConfiguration: models.RepeaterConfiguration{ID: spoof, Callsign: "KD5SMP"}, OwnerID: untrackedRadio},
			{RepeaterConfiguration: models.RepeaterConfiguration{ID: trusted, Callsign: "KD5SMP"}, OwnerID: untrackedRadio, Trusted: true},
			{RepeaterConfiguration: models.RepeaterConfiguration{ID: witnessed, Callsign: "N5WIT"}, OwnerID: witness},
		} {
			require.NoError(t, r.db.Create(&rpt).Error)
		}
		p, token := r.add("Location", rng(5050, 5059))
		s := r.connect(token)
		m, err := recv(t, s)
		require.NoError(t, err)
		require.NotNil(t, m.GetWelcome())
		subscribe(t, s)

		report := func(radio, via uint, stream uint32) {
			t.Helper()
			require.NoError(t, r.registry.Deliver(p.ID, models.Packet{Src: radio, Dst: uint(locatorID), Repeater: via, Slot: true, StreamID: uint(stream)}))
			m, err := recv(t, s)
			require.NoError(t, err)
			require.NotNil(t, m.GetBurst(), "got %v", m)
			require.NoError(t, s.Send(position(radio, locatorID)))
		}
		report(trackedRadio, spoof, 1)
		report(witness, witnessed, 2)
		got := r.nextPosition()
		assert.Equal(t, witness, got.RadioID, "a location report through a hotspot of another user that no admin trusts")

		report(trackedRadio, trusted, 3)
		got = r.nextPosition()
		assert.Equal(t, trackedRadio, got.RadioID)
		assert.Equal(t, trusted, got.RepeaterID, "a location report through a trusted repeater")
		require.NoError(t, r.db.Where("radio_id = ?", trackedRadio).Delete(&models.Position{}).Error)

		report(trackedRadio, own, 4)
		got = r.nextPosition()
		assert.Equal(t, trackedRadio, got.RadioID)
		assert.Equal(t, own, got.RepeaterID, "a location report through a repeater of the owner")
		assert.Len(t, r.positions(), 3)
	})
}
