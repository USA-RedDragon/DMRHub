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

package lastheard_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLastheardShowsDataOnlyInsideTheMessageWindow(t *testing.T) {
	t.Parallel()
	router, tdb, err := testutils.CreateTestDBRouter()
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	database := tdb.DB()
	seedUser(t, database, alice, "alice")
	seedUser(t, database, bob, "bob")

	var user models.User
	require.NoError(t, database.First(&user, alice).Error)
	since := time.Now().Add(-time.Hour)
	require.NoError(t, models.SetSMSStorage(database, &user, true, since))

	sap := uint8(4)
	row := func(stream uint, sub string, received time.Time) {
		toUser := alice
		c := models.Call{
			StreamID: stream, StartTime: received, Kind: models.CallKindData, SubKind: sub, SAP: &sap,
			DataFormat: "ip", Octets: 44, Blocks: 3, Response: models.CallResponseACK,
			SourceID: bob, DestinationID: alice, IsToUser: true, ToUserID: &toUser,
		}
		require.NoError(t, database.Create(&c).Error)
		require.NoError(t, database.Create(&models.Message{
			ReceivedAt: received, TransmissionID: &c.ID, SourceID: bob, DestinationID: alice,
			Format: models.MessageFormatIP, Octets: 44,
		}).Error)
	}
	row(1, models.SubKindSMS, since.Add(time.Minute))
	row(2, models.SubKindSMS, since.Add(-time.Minute))
	row(3, "lrrp", since.Add(-2*time.Minute))

	aliceJar := login(t, router, "alice")
	p := getPage(t, router, "/api/v1/lastheard/user/3191868", &aliceJar)
	require.Len(t, p.Calls, 3)
	byStream := map[uint]int{}
	for i := range p.Calls {
		var c models.Call
		require.NoError(t, database.First(&c, p.Calls[i].ID).Error)
		byStream[c.StreamID] = i
	}
	inside := p.Calls[byStream[1]]
	assert.Equal(t, models.SubKindSMS, inside.SubKind, "an SMS inside the inbox window")
	assert.Equal(t, uint(44), inside.Octets)
	for _, stream := range []uint{2, 3} {
		c := p.Calls[byStream[stream]]
		assert.Equal(t, models.CallKindData, c.Kind)
		assert.Empty(t, c.SubKind, "stream %d is outside the window", stream)
		assert.Zero(t, c.Octets, "stream %d is outside the window", stream)
		assert.Nil(t, c.SAP, "stream %d is outside the window", stream)
		assert.Equal(t, models.CallResponseNone, c.Response, "stream %d is outside the window", stream)
	}

	p = getPage(t, router, "/api/v1/lastheard/user/3191868?types=sms", &aliceJar)
	require.Len(t, p.Calls, 1)
	assert.Equal(t, models.SubKindSMS, p.Calls[0].SubKind)

	_, _, admin := testutils.LoginAdmin(t, router)
	p = getPage(t, router, "/api/v1/lastheard/user/3191868", &admin)
	require.Len(t, p.Calls, 3)
	for _, c := range p.Calls {
		assert.NotEmpty(t, c.SubKind, "an admin sees every row")
		assert.Equal(t, uint(44), c.Octets)
	}
}
