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

func TestVisibleTransmissions(t *testing.T) {
	t.Parallel()
	database, cleanup := openDB(t)
	defer cleanup()

	user := models.User{ID: 3191868, Callsign: "KI5VMF", Username: "a"}
	require.NoError(t, database.Create(&user).Error)
	require.NoError(t, models.SetSMSStorage(database, &user, true, time.Now()))

	var calls [3]models.Call
	for i := range calls {
		calls[i] = models.Call{StartTime: time.Now(), Kind: models.CallKindData}
		require.NoError(t, database.Create(&calls[i]).Error)
	}
	since := *user.SMSStorageSince
	require.NoError(t, database.Create(&models.Message{ReceivedAt: since.Add(-time.Minute), TransmissionID: &calls[0].ID, SourceID: 1, DestinationID: user.ID, Format: models.MessageFormatIP}).Error)
	require.NoError(t, database.Create(&models.Message{ReceivedAt: since.Add(time.Second), TransmissionID: &calls[1].ID, SourceID: 1, DestinationID: user.ID, Format: models.MessageFormatIP}).Error)
	require.NoError(t, database.Create(&models.Message{ReceivedAt: since.Add(time.Second), TransmissionID: &calls[2].ID, SourceID: 1, DestinationID: 2, Format: models.MessageFormatIP}).Error)
	ids := []uint{calls[0].ID, calls[1].ID, calls[2].ID}

	got, err := models.VisibleTransmissions(database, &user, ids)
	require.NoError(t, err)
	assert.Equal(t, map[uint]bool{calls[1].ID: true}, got)

	got, err = models.VisibleTransmissions(database, nil, ids)
	require.NoError(t, err)
	assert.Empty(t, got)

	admin := models.User{ID: 1, Admin: true}
	got, err = models.VisibleTransmissions(database, &admin, ids)
	require.NoError(t, err)
	assert.Len(t, got, 3)

	require.NoError(t, models.SetSMSStorage(database, &user, false, time.Now()))
	got, err = models.VisibleTransmissions(database, &user, ids)
	require.NoError(t, err)
	assert.Empty(t, got)
}
