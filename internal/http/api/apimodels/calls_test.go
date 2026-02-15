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

package apimodels_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/stretchr/testify/assert"
)

func TestNewWSCallResponseFromCall_Talkgroup(t *testing.T) {
	t.Parallel()

	tgID := uint(9990)
	call := &models.Call{
		ID:            42,
		StartTime:     time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC),
		Duration:      5 * time.Second,
		Active:        true,
		TimeSlot:      false,
		GroupCall:     true,
		IsToTalkgroup: true,
		ToTalkgroupID: &tgID,
		ToTalkgroup: models.Talkgroup{
			ID:          9990,
			Name:        "Parrot",
			Description: "Echo test",
		},
		IsToUser:     false,
		IsToRepeater: false,
		Loss:         0.01,
		Jitter:       2.5,
		BER:          0.001,
		RSSI:         -60,
		User: models.User{
			ID:       100001,
			Callsign: "T3ST",
		},
	}

	resp := apimodels.NewWSCallResponseFromCall(call)

	assert.Equal(t, uint(42), resp.ID)
	assert.Equal(t, uint(100001), resp.User.ID)
	assert.Equal(t, "T3ST", resp.User.Callsign)
	assert.Equal(t, call.StartTime, resp.StartTime)
	assert.Equal(t, 5*time.Second, resp.Duration)
	assert.True(t, resp.Active)
	assert.False(t, resp.TimeSlot)
	assert.True(t, resp.GroupCall)
	assert.True(t, resp.IsToTalkgroup)
	assert.Equal(t, uint(9990), resp.ToTalkgroup.ID)
	assert.Equal(t, "Parrot", resp.ToTalkgroup.Name)
	assert.Equal(t, "Echo test", resp.ToTalkgroup.Description)
	assert.False(t, resp.IsToUser)
	assert.False(t, resp.IsToRepeater)
	// ToUser and ToRepeater should be zero values
	assert.Equal(t, uint(0), resp.ToUser.ID)
	assert.Equal(t, uint(0), resp.ToRepeater.RadioID)
	assert.InDelta(t, float32(0.01), resp.Loss, 0.0001)
	assert.InDelta(t, float32(2.5), resp.Jitter, 0.0001)
}

func TestNewWSCallResponseFromCall_User(t *testing.T) {
	t.Parallel()

	toUserID := uint(200001)
	call := &models.Call{
		ID:        99,
		Active:    false,
		GroupCall: false,
		IsToUser:  true,
		ToUserID:  &toUserID,
		ToUser: models.User{
			ID:       200001,
			Callsign: "DST1",
		},
		IsToTalkgroup: false,
		IsToRepeater:  false,
		User: models.User{
			ID:       100001,
			Callsign: "SRC1",
		},
	}

	resp := apimodels.NewWSCallResponseFromCall(call)

	assert.Equal(t, uint(99), resp.ID)
	assert.True(t, resp.IsToUser)
	assert.Equal(t, uint(200001), resp.ToUser.ID)
	assert.Equal(t, "DST1", resp.ToUser.Callsign)
	assert.False(t, resp.IsToTalkgroup)
	assert.False(t, resp.IsToRepeater)
	// ToTalkgroup and ToRepeater should be zero values
	assert.Equal(t, uint(0), resp.ToTalkgroup.ID)
	assert.Equal(t, uint(0), resp.ToRepeater.RadioID)
}

func TestNewWSCallResponseFromCall_Repeater(t *testing.T) {
	t.Parallel()

	toRepID := uint(311999)
	call := &models.Call{
		ID:           77,
		Active:       true,
		GroupCall:    true,
		IsToRepeater: true,
		ToRepeaterID: &toRepID,
		ToRepeater: models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{
				ID:       311999,
				Callsign: "REP1",
			},
		},
		IsToTalkgroup: false,
		IsToUser:      false,
		User: models.User{
			ID:       100001,
			Callsign: "SRC1",
		},
	}

	resp := apimodels.NewWSCallResponseFromCall(call)

	assert.Equal(t, uint(77), resp.ID)
	assert.True(t, resp.IsToRepeater)
	assert.Equal(t, uint(311999), resp.ToRepeater.RadioID)
	assert.Equal(t, "REP1", resp.ToRepeater.Callsign)
	assert.False(t, resp.IsToTalkgroup)
	assert.False(t, resp.IsToUser)
}
