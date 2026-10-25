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

package positions_test

import (
	"context"
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const (
	owner    = uint(3191868)
	sharer   = uint(3140598)
	private  = uint(3100001)
	blocked  = uint(3100002)
	repeater = uint(319186801)
)

func rig(t *testing.T) (*gorm.DB, pubsub.PubSub, *positions.Pusher) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_pos_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})
	for _, u := range []models.User{
		{ID: owner, Callsign: "KI5VMF", Username: "a", Approved: true},
		{ID: sharer, Callsign: "KD5SMP", Username: "b", Approved: true},
		{ID: private, Callsign: "N0CALL", Username: "c", Approved: true},
		{ID: blocked, Callsign: "N0BAD", Username: "d", Approved: true, Suspended: true},
	} {
		require.NoError(t, database.Create(&u).Error)
	}
	require.NoError(t, models.SetSharePositions(database, sharer, true))
	require.NoError(t, models.SetSharePositions(database, blocked, true))
	require.NoError(t, models.SetPositionTracking(database, owner, owner, true, time.Now().Add(-time.Minute)))
	return database, ps, positions.NewPusher(database, ps)
}

func frames(t *testing.T, sub pubsub.Subscription) []apimodels.PositionPush {
	t.Helper()
	var out []apimodels.PositionPush
	deadline := time.After(300 * time.Millisecond)
	for {
		select {
		case b := <-sub.Channel():
			var f apimodels.PositionPush
			require.NoError(t, json.Unmarshal(b, &f))
			out = append(out, f)
		case <-deadline:
			return out
		}
	}
}

func position() models.Position {
	return models.Position{RadioID: owner, Latitude: 30.2672, Longitude: -97.7431, Format: models.PositionFormatLRRP,
		ReceivedAt: time.Now(), RepeaterID: repeater, Slot: 2, ServiceID: 5057}
}

func TestAPositionGoesToItsOwnerOnlyUntilTheyShare(t *testing.T) {
	t.Parallel()
	database, ps, p := rig(t)
	subs := map[uint]pubsub.Subscription{}
	for _, id := range []uint{owner, sharer, private, blocked} {
		subs[id] = ps.Subscribe(positions.Topic(id))
		defer func() { _ = subs[id].Close() }()
	}

	p.Push(position())
	mine := frames(t, subs[owner])
	require.Len(t, mine, 1)
	assert.Equal(t, "position", mine[0].Kind)
	assert.Equal(t, owner, mine[0].Position.RadioID)
	assert.False(t, mine[0].Position.Shared)
	assert.Empty(t, frames(t, subs[sharer]), "the owner does not share")

	require.NoError(t, models.SetSharePositions(database, owner, true))
	p.Push(position())
	require.Len(t, frames(t, subs[owner]), 1)
	theirs := frames(t, subs[sharer])
	require.Len(t, theirs, 1)
	assert.True(t, theirs[0].Position.Shared)
	assert.Equal(t, "KI5VMF", theirs[0].Position.Callsign)
	viewer := frames(t, subs[private])
	require.Len(t, viewer, 1, "a user who does not share still sees shared radios")
	assert.True(t, viewer[0].Position.Shared)
	assert.Empty(t, frames(t, subs[blocked]), "this user is suspended")
}

func TestAPositionOfAnUntrackedRadioGoesNowhere(t *testing.T) {
	t.Parallel()
	database, ps, p := rig(t)
	sub := ps.Subscribe(positions.Topic(owner))
	defer func() { _ = sub.Close() }()
	require.NoError(t, models.SetPositionTracking(database, owner, owner, false, time.Now()))
	p.Push(position())
	assert.Empty(t, frames(t, sub))
}

func TestEveryPositionGoesOnTheTopicOfAll(t *testing.T) {
	t.Parallel()
	_, ps, p := rig(t)
	sub := ps.Subscribe(positions.AllTopic)
	defer func() { _ = sub.Close() }()
	pos := position()
	acc := 12.5
	pos.AccuracyM = &acc
	p.Push(pos)
	select {
	case b := <-sub.Channel():
		var got models.Position
		require.NoError(t, json.Unmarshal(b, &got))
		assert.Equal(t, owner, got.RadioID)
		assert.Equal(t, uint(5057), got.ServiceID)
		require.NotNil(t, got.AccuracyM)
		assert.InDelta(t, 12.5, *got.AccuracyM, 1e-9)
	case <-time.After(time.Second):
		t.Fatal("no position on the topic of all")
	}
}
