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

package rules_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/rules"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"gorm.io/gorm"
)

func makeTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	assert.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	assert.NoError(t, err)

	cleanup := func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}
	return database, cleanup
}

func TestPeerShouldEgressNoEgress(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      1,
		Egress:  false,
		Ingress: false,
	}
	database.Create(&peer)

	packet := &models.Packet{
		Src: 100,
		Dst: 200,
	}

	result := rules.PeerShouldEgress(database, peer, packet)
	assert.False(t, result, "peer with egress disabled should not egress")
}

func TestPeerShouldEgressWithMatchingRule(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      2,
		Egress:  true,
		Ingress: false,
	}
	database.Create(&peer)

	rule := models.PeerRule{
		PeerID:       2,
		Direction:    false,
		SubjectIDMin: 100,
		SubjectIDMax: 200,
	}
	database.Create(&rule)

	packet := &models.Packet{
		Src: 150,
		Dst: 300,
	}

	result := rules.PeerShouldEgress(database, peer, packet)
	assert.True(t, result, "peer with matching egress rule should egress")
}

func TestPeerShouldEgressNoMatchingRule(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      3,
		Egress:  true,
		Ingress: false,
	}
	database.Create(&peer)

	rule := models.PeerRule{
		PeerID:       3,
		Direction:    false,
		SubjectIDMin: 100,
		SubjectIDMax: 200,
	}
	database.Create(&rule)

	packet := &models.Packet{
		Src: 500,
		Dst: 300,
	}

	result := rules.PeerShouldEgress(database, peer, packet)
	assert.False(t, result, "peer without matching egress rule should not egress")
}

func TestPeerShouldIngressNoIngress(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      4,
		Egress:  false,
		Ingress: false,
	}
	database.Create(&peer)

	packet := &models.Packet{
		Src: 100,
		Dst: 200,
	}

	result := rules.PeerShouldIngress(database, &peer, packet)
	assert.False(t, result, "peer with ingress disabled should not ingress")
}

func TestPeerShouldIngressWithMatchingRule(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      5,
		Egress:  false,
		Ingress: true,
	}
	database.Create(&peer)

	rule := models.PeerRule{
		PeerID:       5,
		Direction:    true,
		SubjectIDMin: 100,
		SubjectIDMax: 300,
	}
	database.Create(&rule)

	packet := &models.Packet{
		Src: 500,
		Dst: 200,
	}

	result := rules.PeerShouldIngress(database, &peer, packet)
	assert.True(t, result, "peer with matching ingress rule should ingress")
}

func TestPeerShouldIngressNoMatchingRule(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:      6,
		Egress:  false,
		Ingress: true,
	}
	database.Create(&peer)

	rule := models.PeerRule{
		PeerID:       6,
		Direction:    true,
		SubjectIDMin: 100,
		SubjectIDMax: 200,
	}
	database.Create(&rule)

	packet := &models.Packet{
		Src: 500,
		Dst: 500,
	}

	result := rules.PeerShouldIngress(database, &peer, packet)
	assert.False(t, result, "peer without matching ingress rule should not ingress")
}

func TestPeerShouldEgressBoundaryValues(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peer := models.Peer{
		ID:     7,
		Egress: true,
	}
	database.Create(&peer)

	rule := models.PeerRule{
		PeerID:       7,
		Direction:    false,
		SubjectIDMin: 100,
		SubjectIDMax: 200,
	}
	database.Create(&rule)

	packetLow := &models.Packet{Src: 100, Dst: 300}
	assert.True(t, rules.PeerShouldEgress(database, peer, packetLow), "Src at lower bound should match")

	packetHigh := &models.Packet{Src: 200, Dst: 300}
	assert.True(t, rules.PeerShouldEgress(database, peer, packetHigh), "Src at upper bound should match")

	packetBelow := &models.Packet{Src: 99, Dst: 300}
	assert.False(t, rules.PeerShouldEgress(database, peer, packetBelow), "Src below range should not match")

	packetAbove := &models.Packet{Src: 201, Dst: 300}
	assert.False(t, rules.PeerShouldEgress(database, peer, packetAbove), "Src above range should not match")
}
