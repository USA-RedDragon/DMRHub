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

package models_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCallStoresKindAndDataFields(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	sap, fid, opcode, posErr := uint8(4), uint8(0x10), uint8(0x24), uint8(1)
	lat, lon := 32.7767, -96.797
	call := models.Call{
		StreamID:      1,
		StartTime:     time.Now(),
		Kind:          models.CallKindData,
		SubKind:       "sms",
		SourceID:      3140598,
		Transport:     models.RepeaterTypeMMDVM,
		IngressID:     314059801,
		DestinationID: 3191868,
		Bursts:        9,
		FID:           &fid,
		Opcode:        &opcode,
		SAP:           &sap,
		DataFormat:    "ip",
		Confirmed:     true,
		Blocks:        3,
		Octets:        44,
		BadCRC:        1,
		LostBlocks:    2,
		Response:      models.CallResponseACK,
		TalkerAlias:   "KI5VMF Jacob",
		AliasFormat:   "7-bit",
		Latitude:      &lat,
		Longitude:     &lon,
		PositionError: &posErr,
		Partial:       true,
		Undeliverable: []string{"ipsc:311304200"},
	}
	require.NoError(t, database.Create(&call).Error)

	var got models.Call
	require.NoError(t, database.First(&got, call.ID).Error)
	assert.Nil(t, got.UserID, "a source that is not a user has no user")
	assert.Nil(t, got.RepeaterID, "an ingress that is not a repeater has no repeater")
	assert.Equal(t, call.Kind, got.Kind)
	assert.Equal(t, call.SubKind, got.SubKind)
	assert.Equal(t, call.SourceID, got.SourceID)
	assert.Equal(t, call.Transport, got.Transport)
	assert.Equal(t, call.IngressID, got.IngressID)
	assert.Equal(t, call.Bursts, got.Bursts)
	assert.Equal(t, call.FID, got.FID)
	assert.Equal(t, call.Opcode, got.Opcode)
	assert.Equal(t, call.SAP, got.SAP)
	assert.Equal(t, call.DataFormat, got.DataFormat)
	assert.Equal(t, call.Confirmed, got.Confirmed)
	assert.Equal(t, call.Blocks, got.Blocks)
	assert.Equal(t, call.Octets, got.Octets)
	assert.Equal(t, call.BadCRC, got.BadCRC)
	assert.Equal(t, call.LostBlocks, got.LostBlocks)
	assert.Equal(t, call.Response, got.Response)
	assert.Equal(t, call.TalkerAlias, got.TalkerAlias)
	assert.Equal(t, call.AliasFormat, got.AliasFormat)
	assert.Equal(t, call.Latitude, got.Latitude)
	assert.Equal(t, call.Longitude, got.Longitude)
	assert.Equal(t, call.PositionError, got.PositionError)
	assert.Equal(t, call.Partial, got.Partial)
	assert.Equal(t, call.Undeliverable, got.Undeliverable)
}

func TestCallKindDefaultsToVoice(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	call := models.Call{StreamID: 1, StartTime: time.Now()}
	require.NoError(t, database.Create(&call).Error)
	var got models.Call
	require.NoError(t, database.First(&got, call.ID).Error)
	assert.Equal(t, models.CallKindVoice, got.Kind)
}

func TestNetCallsCountVoiceOnly(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	const tg = uint(3100)
	require.NoError(t, database.Create(&models.Talkgroup{ID: tg, Name: "USA"}).Error)
	start := time.Now().Add(-time.Hour)
	toTG := tg
	for i, kind := range []models.CallKind{models.CallKindVoice, models.CallKindData, models.CallKindCSBK, models.CallKindGPS} {
		userID := uint(1000001 + i)
		require.NoError(t, database.Create(&models.User{ID: userID, Callsign: "N0CAL" + string(rune('A'+i)), Username: string(kind)}).Error)
		require.NoError(t, database.Create(&models.Call{
			StreamID: uint(i + 1), StartTime: start.Add(time.Minute), Kind: kind, UserID: &userID, SourceID: userID,
			GroupCall: true, DestinationID: tg, IsToTalkgroup: true, ToTalkgroupID: &toTG,
		}).Error)
	}

	require.NoError(t, database.Create(&models.Call{
		StreamID: 9, StartTime: start.Add(time.Minute), Kind: models.CallKindVoice, SourceID: 3999999,
		GroupCall: true, DestinationID: tg, IsToTalkgroup: true, ToTalkgroupID: &toTG,
	}).Error)

	end := time.Now()
	calls, err := models.FindTalkgroupCallsInTimeRange(database, tg, start, end)
	require.NoError(t, err)
	require.Len(t, calls, 1)
	assert.Equal(t, models.CallKindVoice, calls[0].Kind)
	count, err := models.CountTalkgroupCallsInTimeRange(database, tg, start, end)
	require.NoError(t, err)
	assert.Equal(t, 1, count)
	users, err := models.CountUniqueUsersInTimeRange(database, tg, start, end)
	require.NoError(t, err)
	assert.Equal(t, 1, users)
}
