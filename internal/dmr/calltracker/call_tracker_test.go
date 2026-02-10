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

package calltracker_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
)

func makeTestCallTracker(t *testing.T) (*calltracker.CallTracker, func()) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	ct := calltracker.NewCallTracker(database, ps)

	cleanup := func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}
	return ct, cleanup
}

func TestNewCallTracker(t *testing.T) {
	t.Parallel()
	ct, cleanup := makeTestCallTracker(t)
	defer cleanup()

	assert.NotNil(t, ct)
}

func TestStartCallNonExistentUser(t *testing.T) {
	t.Parallel()
	ct, cleanup := makeTestCallTracker(t)
	defer cleanup()

	packet := models.Packet{
		Src:       999999,
		Dst:       1,
		Repeater:  1,
		GroupCall: true,
		StreamID:  12345,
		Slot:      false,
	}

	// Should silently return (user doesn't exist)
	assert.NotPanics(t, func() {
		ct.StartCall(context.Background(), packet)
	})
}

func TestStartCallNonExistentRepeater(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	ct := calltracker.NewCallTracker(database, ps)

	err = database.Create(&models.User{
		ID:       100001,
		Username: "testuser",
		Callsign: "T3ST",
		Approved: true,
	}).Error
	assert.NoError(t, err)

	packet := models.Packet{
		Src:       100001,
		Dst:       1,
		Repeater:  999999,
		GroupCall: true,
		StreamID:  12345,
		Slot:      false,
	}

	// Should not panic even with non-existent repeater
	assert.NotPanics(t, func() {
		ct.StartCall(context.Background(), packet)
	})
}

func TestCallTrackerMultipleInstances(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	assert.NoError(t, err)

	ct1 := calltracker.NewCallTracker(database, ps)
	ct2 := calltracker.NewCallTracker(database, ps)

	assert.NotNil(t, ct1)
	assert.NotNil(t, ct2)
	// Different instances
	assert.NotSame(t, ct1, ct2)
}

func TestCallTrackerEndCallNonExistentCall(t *testing.T) {
	t.Parallel()
	ct, cleanup := makeTestCallTracker(t)
	defer cleanup()

	packet := models.Packet{
		Src:       999999,
		Dst:       1,
		Repeater:  1,
		GroupCall: true,
		StreamID:  99999,
		Slot:      false,
	}

	// EndCall on non-existent call should not panic
	assert.NotPanics(t, func() {
		ct.EndCall(context.Background(), packet)
	})
}

func TestCallTrackerIsCallActive(t *testing.T) {
	t.Parallel()
	ct, cleanup := makeTestCallTracker(t)
	defer cleanup()

	packet := models.Packet{
		Src:       999999,
		Dst:       1,
		Repeater:  1,
		GroupCall: true,
		StreamID:  12345,
		Slot:      false,
	}

	// No call started, should not be active
	active := ct.IsCallActive(context.Background(), packet)
	// We expect false since no call exists
	assert.False(t, active, fmt.Sprintf("expected call not to be active, got %v", active))
}
