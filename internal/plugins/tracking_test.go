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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
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
func trackingRig(t *testing.T) (*rig, pluginv1.PluginService_ConnectClient) {
	t.Helper()
	r := newRig(t)
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
	_, s := trackingRig(t)
	radios := subscribe(t, s)
	require.Len(t, radios, 1)
	assert.Equal(t, uint32(trackedRadio), radios[0].GetRadio())
	assert.True(t, radios[0].GetOn())
	assert.Equal(t, uint32(600), radios[0].GetIntervalSeconds())
}

func TestAPluginHearsOnlyTrackedRadiosAndOnlyWhenSubscribed(t *testing.T) {
	t.Parallel()
	r, s := trackingRig(t)
	require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100001, 2, 7))
	subscribe(t, s)
	require.NoError(t, plugins.PublishHeard(r.ps, untrackedRadio, 100001, 1, 1))
	require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
	m, err := recv(t, s)
	require.NoError(t, err)
	assert.True(t, proto.Equal(&pluginv1.Heard{Radio: uint32(trackedRadio), Slot: 2, ColorCode: 7}, m.GetHeard()), "got %v", m)
}

func TestAPluginHearsWhenAnOwnerChangesTracking(t *testing.T) {
	t.Parallel()
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
	r, s := trackingRig(t)
	subscribe(t, s)
	require.NoError(t, plugins.PublishHeard(r.ps, trackedRadio, 100002, 2, 7))
	_, err := recv(t, s)
	require.NoError(t, err)

	require.NoError(t, s.Send(position(untrackedRadio, locatorID)))
	require.NoError(t, s.Send(position(trackedRadio, 9990)))
	require.NoError(t, s.Send(position(trackedRadio, locatorID)))
	require.Eventually(t, func() bool { return len(r.positions()) == 1 }, 3*time.Second, 10*time.Millisecond)
	time.Sleep(100 * time.Millisecond)
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
}
