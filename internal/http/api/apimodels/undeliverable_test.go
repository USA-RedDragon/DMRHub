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
	"fmt"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestUndeliverableOwnersOfPeersAndRemovedRepeaters checks the owners that
// the undeliverable list is shown to when a sentence names an OpenBridge
// peer, a repeater that was deleted, an ID that is both a repeater and a
// peer, or no repeater at all.
func TestUndeliverableOwnersOfPeersAndRemovedRepeaters(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:memdb_undeliverable_%p", t)
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	})

	const peerOwner, repeaterOwner, gone = 3150001, 3150002, 3150003
	for _, id := range []uint{peerOwner, repeaterOwner, gone} {
		require.NoError(t, database.Create(&models.User{ID: id, Callsign: fmt.Sprintf("U%d", id), Username: fmt.Sprintf("u%d", id), Approved: true}).Error)
	}
	require.NoError(t, database.Create(&models.Peer{ID: 311000, OwnerID: peerOwner}).Error)
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311000, Callsign: "W1RPT"}, OwnerID: repeaterOwner, Type: models.RepeaterTypeMMDVM}).Error)
	removed := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: 311001, Callsign: "W1GON"}, OwnerID: gone, Type: models.RepeaterTypeIPSC}
	require.NoError(t, database.Create(&removed).Error)
	require.NoError(t, database.Delete(&removed).Error)

	calls := []models.Call{{ID: 1, SourceID: 3191868, GroupCall: true, DestinationID: 91, Undeliverable: []string{
		"OpenBridge peer 311000 was busy on that slot.",
		"MMDVM repeater 311000 was busy on that slot.",
		"IPSC repeater 311001 was busy on that slot.",
		"An IPSC repeater was busy on that slot.",
		"Something the hub did not write.",
	}}}
	for _, tc := range []struct {
		name   string
		viewer *models.User
		want   []string
		scope  string
	}{
		{"peer owner", &models.User{ID: peerOwner, Approved: true}, []string{"OpenBridge peer 311000 was busy on that slot."}, apimodels.UndeliverableScopeOwn},
		{"repeater owner", &models.User{ID: repeaterOwner, Approved: true}, []string{"MMDVM repeater 311000 was busy on that slot."}, apimodels.UndeliverableScopeOwn},
		{"owner of a deleted repeater", &models.User{ID: gone, Approved: true}, nil, ""},
		{"caller", &models.User{ID: 3191868, Approved: true}, calls[0].Undeliverable, apimodels.UndeliverableScopeAll},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rows, err := apimodels.NewWSCallResponsesFor(database, tc.viewer, models.MaskedSMSWindow{}, calls)
			require.NoError(t, err)
			require.Len(t, rows, 1)
			var got []string
			for _, e := range rows[0].Undeliverable {
				got = append(got, e.Sentence)
			}
			assert.Equal(t, tc.want, got)
			assert.Equal(t, tc.scope, rows[0].UndeliverableScope)
		})
	}

	rows, err := apimodels.NewWSCallResponsesFor(database, &models.User{ID: 3191868, Approved: true}, models.MaskedSMSWindow{}, calls)
	require.NoError(t, err)
	assert.Equal(t, []apimodels.WSCallUndeliverable{
		{Sentence: "OpenBridge peer 311000 was busy on that slot.", Target: "openbridge", RepeaterID: 311000, Reason: "slot_busy"},
		{Sentence: "MMDVM repeater 311000 was busy on that slot.", Target: "mmdvm", RepeaterID: 311000, Callsign: "W1RPT", Reason: "slot_busy"},
		{Sentence: "IPSC repeater 311001 was busy on that slot.", Target: "ipsc", RepeaterID: 311001, Reason: "slot_busy"},
		{Sentence: "An IPSC repeater was busy on that slot.", Target: "ipsc", Reason: "slot_busy"},
		{Sentence: "Something the hub did not write."},
	}, rows[0].Undeliverable, "the caller sees each entry with its parts and the callsign of each repeater that is still there")
}
