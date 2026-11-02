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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func createPeers(t *testing.T, database *gorm.DB, ids ...uint) {
	t.Helper()
	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "owner"}
	require.NoError(t, database.Create(&owner).Error)
	for _, id := range ids {
		require.NoError(t, database.Create(&models.Peer{ID: id, OwnerID: owner.ID, Ingress: true, Egress: true}).Error)
	}
}

func TestListRulesForPeerEmpty(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	rules, err := models.ListRulesForPeer(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, rules)
}

func TestListRulesForPeerReturnsRules(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	createPeers(t, database, 10)

	ingress := models.PeerRule{PeerID: 10, Direction: true, SubjectIDMin: 100, SubjectIDMax: 200}
	egress := models.PeerRule{PeerID: 10, Direction: false, SubjectIDMin: 300, SubjectIDMax: 400}
	require.NoError(t, database.Create(&ingress).Error)
	require.NoError(t, database.Create(&egress).Error)

	rules, err := models.ListRulesForPeer(database, 10)
	assert.NoError(t, err)
	assert.Len(t, rules, 2)
}

func TestListIngressRulesForPeerEmpty(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	rules, err := models.ListIngressRulesForPeer(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, rules)
}

func TestListIngressRulesForPeerFiltersDirection(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	createPeers(t, database, 11)

	ingress := models.PeerRule{PeerID: 11, Direction: true, SubjectIDMin: 100, SubjectIDMax: 200}
	egress := models.PeerRule{PeerID: 11, Direction: false, SubjectIDMin: 300, SubjectIDMax: 400}
	require.NoError(t, database.Create(&ingress).Error)
	require.NoError(t, database.Create(&egress).Error)

	rules, err := models.ListIngressRulesForPeer(database, 11)
	assert.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.True(t, rules[0].Direction, "should only return ingress rules")
}

func TestListEgressRulesForPeerEmpty(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	rules, err := models.ListEgressRulesForPeer(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, rules)
}

func TestListEgressRulesForPeerFiltersDirection(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	createPeers(t, database, 12)

	ingress := models.PeerRule{PeerID: 12, Direction: true, SubjectIDMin: 100, SubjectIDMax: 200}
	egress := models.PeerRule{PeerID: 12, Direction: false, SubjectIDMin: 300, SubjectIDMax: 400}
	require.NoError(t, database.Create(&ingress).Error)
	require.NoError(t, database.Create(&egress).Error)

	rules, err := models.ListEgressRulesForPeer(database, 12)
	assert.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.False(t, rules[0].Direction, "should only return egress rules")
}

func TestListRulesForPeerDoesNotReturnOtherPeerRules(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	createPeers(t, database, 13, 14)

	rule1 := models.PeerRule{PeerID: 13, Direction: true, SubjectIDMin: 100, SubjectIDMax: 200}
	rule2 := models.PeerRule{PeerID: 14, Direction: true, SubjectIDMin: 300, SubjectIDMax: 400}
	require.NoError(t, database.Create(&rule1).Error)
	require.NoError(t, database.Create(&rule2).Error)

	rules, err := models.ListRulesForPeer(database, 13)
	assert.NoError(t, err)
	assert.Len(t, rules, 1)
	assert.Equal(t, uint(13), rules[0].PeerID)
}
