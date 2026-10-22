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
	"encoding/json"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMessageStoresEveryField(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	call := models.Call{StartTime: time.Now(), Kind: models.CallKindData, SourceID: 3140598, DestinationID: 3191868}
	require.NoError(t, database.Create(&call).Error)

	sap, ns, fsn := uint8(4), uint8(3), uint8(0)
	text := "hello 73"
	at := time.Date(2026, 10, 22, 19, 22, 5, 0, time.UTC)
	msg := models.Message{
		ReceivedAt:     at,
		TransmissionID: &call.ID,
		SourceID:       3140598,
		DestinationID:  3191868,
		RepeaterID:     314059801,
		Slot:           2,
		Transport:      models.RepeaterTypeMMDVM,
		StreamID:       77,
		SAP:            &sap,
		Format:         models.MessageFormatIP,
		Confirmed:      true,
		SendSequence:   &ns,
		FragmentSeq:    &fsn,
		Response:       models.CallResponseACK,
		Text:           &text,
		ChecksumOK:     false,
		Blocks:         4,
		Octets:         60,
		Raw:            []byte{0x42, 0x01, 0x02},
	}
	require.NoError(t, database.Create(&msg).Error)

	var got models.Message
	require.NoError(t, database.First(&got, msg.ID).Error)
	assert.True(t, got.ReceivedAt.Equal(at))
	require.NotNil(t, got.TransmissionID)
	assert.Equal(t, call.ID, *got.TransmissionID)
	assert.Equal(t, msg.SourceID, got.SourceID)
	assert.Equal(t, msg.DestinationID, got.DestinationID)
	assert.False(t, got.IsGroup)
	assert.Equal(t, msg.RepeaterID, got.RepeaterID)
	assert.Equal(t, msg.Slot, got.Slot)
	assert.Equal(t, msg.Transport, got.Transport)
	assert.Equal(t, msg.StreamID, got.StreamID)
	assert.Equal(t, msg.SAP, got.SAP)
	assert.Equal(t, msg.Format, got.Format)
	assert.True(t, got.Confirmed)
	assert.Equal(t, msg.SendSequence, got.SendSequence)
	assert.Equal(t, msg.FragmentSeq, got.FragmentSeq)
	assert.Equal(t, models.CallResponseACK, got.Response)
	require.NotNil(t, got.Text)
	assert.Equal(t, text, *got.Text)
	assert.False(t, got.ChecksumOK)
	assert.Equal(t, msg.Blocks, got.Blocks)
	assert.Equal(t, msg.Octets, got.Octets)
	assert.Equal(t, msg.Raw, got.Raw)
}

func TestMessageWithoutTextStoresNull(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	msg := models.Message{ReceivedAt: time.Now(), SourceID: 1, DestinationID: 2, Format: models.MessageFormatData, ChecksumOK: true}
	require.NoError(t, database.Create(&msg).Error)
	var got models.Message
	require.NoError(t, database.First(&got, msg.ID).Error)
	assert.Nil(t, got.Text)
	assert.True(t, got.ChecksumOK)
}

func TestUserRadioIDIsUnique(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "a"}).Error)
	require.NoError(t, database.Create(&models.User{ID: 3140598, Callsign: "KD5SMP", Username: "b"}).Error)
	require.NoError(t, database.Create(&models.UserRadioID{UserID: 3191868, RadioID: 3191877, VerifiedAt: time.Now()}).Error)
	assert.Error(t, database.Create(&models.UserRadioID{UserID: 3140598, RadioID: 3191877, VerifiedAt: time.Now()}).Error)
}

func TestDeleteUserDeletesTheirRadioIDs(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	require.NoError(t, database.Create(&models.User{ID: 3191868, Callsign: "KI5VMF", Username: "a"}).Error)
	require.NoError(t, database.Create(&models.UserRadioID{UserID: 3191868, RadioID: 3191877, VerifiedAt: time.Now()}).Error)
	require.NoError(t, models.DeleteUser(database, 3191868))
	var n int64
	require.NoError(t, database.Model(&models.UserRadioID{}).Count(&n).Error)
	assert.Zero(t, n)
}

func TestUserJSONHasNoSMSStorageFields(t *testing.T) {
	t.Parallel()
	now := time.Now()
	b, err := json.Marshal(models.User{ID: 3191868, Callsign: "KI5VMF", SMSStorageEnabled: true, SMSStorageSince: &now})
	require.NoError(t, err)
	var keys map[string]any
	require.NoError(t, json.Unmarshal(b, &keys))
	for k := range keys {
		assert.NotContains(t, k, "sms", "key %q", k)
	}
}

func TestDeletingATransmissionKeepsItsMessages(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	call := models.Call{StartTime: time.Now(), Kind: models.CallKindData}
	require.NoError(t, database.Create(&call).Error)
	msg := models.Message{ReceivedAt: time.Now(), TransmissionID: &call.ID, Format: models.MessageFormatData}
	require.NoError(t, database.Create(&msg).Error)
	require.NoError(t, database.Unscoped().Delete(&call).Error)
	var got models.Message
	require.NoError(t, database.First(&got, msg.ID).Error)
}
