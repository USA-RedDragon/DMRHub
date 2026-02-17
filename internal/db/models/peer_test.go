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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func makeTestDB(t *testing.T) (*gorm.DB, func()) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	cleanup := func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}
	return database, cleanup
}

func TestListPeersReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	// Empty DB should return empty list and no error
	peers, err := models.ListPeers(database)
	assert.NoError(t, err)
	assert.Empty(t, peers)
}

func TestListPeersReturnsPeers(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "testuser"}
	database.Create(&owner)

	peer := models.Peer{ID: 100, OwnerID: owner.ID, Ingress: true, Egress: true}
	database.Create(&peer)

	peers, err := models.ListPeers(database)
	assert.NoError(t, err)
	assert.Len(t, peers, 1)
	assert.Equal(t, uint(100), peers[0].ID)
}

func TestCountPeersReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	count, err := models.CountPeers(database)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountPeersReturnsCount(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "testuser"}
	database.Create(&owner)

	database.Create(&models.Peer{ID: 100, OwnerID: owner.ID})
	database.Create(&models.Peer{ID: 101, OwnerID: owner.ID})

	count, err := models.CountPeers(database)
	assert.NoError(t, err)
	assert.Equal(t, 2, count)
}

func TestGetUserPeersReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	peers, err := models.GetUserPeers(database, 999)
	assert.NoError(t, err)
	assert.Empty(t, peers)
}

func TestGetUserPeersFiltersCorrectly(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner1 := models.User{ID: 1, Callsign: "N0CALL", Username: "user1"}
	owner2 := models.User{ID: 2, Callsign: "N1CALL", Username: "user2"}
	database.Create(&owner1)
	database.Create(&owner2)

	database.Create(&models.Peer{ID: 100, OwnerID: owner1.ID})
	database.Create(&models.Peer{ID: 101, OwnerID: owner2.ID})
	database.Create(&models.Peer{ID: 102, OwnerID: owner1.ID})

	peers, err := models.GetUserPeers(database, owner1.ID)
	assert.NoError(t, err)
	assert.Len(t, peers, 2)
	for _, p := range peers {
		assert.Equal(t, owner1.ID, p.OwnerID)
	}
}

func TestCountUserPeersReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	count, err := models.CountUserPeers(database, 999)
	assert.NoError(t, err)
	assert.Equal(t, 0, count)
}

func TestCountUserPeersFiltersCorrectly(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner1 := models.User{ID: 1, Callsign: "N0CALL", Username: "user1"}
	owner2 := models.User{ID: 2, Callsign: "N1CALL", Username: "user2"}
	database.Create(&owner1)
	database.Create(&owner2)

	database.Create(&models.Peer{ID: 100, OwnerID: owner1.ID})
	database.Create(&models.Peer{ID: 101, OwnerID: owner2.ID})

	count, err := models.CountUserPeers(database, owner1.ID)
	assert.NoError(t, err)
	assert.Equal(t, 1, count)
}

func TestFindPeerByIDReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	// Non-existent peer should return an error
	_, err := models.FindPeerByID(database, 999)
	assert.Error(t, err)
}

func TestFindPeerByIDReturnsPeer(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "testuser"}
	database.Create(&owner)
	database.Create(&models.Peer{ID: 100, OwnerID: owner.ID, Ingress: true, Egress: false})

	peer, err := models.FindPeerByID(database, 100)
	assert.NoError(t, err)
	assert.Equal(t, uint(100), peer.ID)
	assert.True(t, peer.Ingress)
	assert.False(t, peer.Egress)
}

func TestPeerIDExistsReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	// Non-existent peer
	exists, err := models.PeerIDExists(database, 999)
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestPeerIDExistsReturnsTrue(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "testuser"}
	database.Create(&owner)
	database.Create(&models.Peer{ID: 100, OwnerID: owner.ID})

	exists, err := models.PeerIDExists(database, 100)
	assert.NoError(t, err)
	assert.True(t, exists)
}

func TestDeletePeerReturnsError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "testuser"}
	database.Create(&owner)
	database.Create(&models.Peer{ID: 100, OwnerID: owner.ID})

	err := models.DeletePeer(database, 100)
	assert.NoError(t, err)

	// Verify the peer was deleted
	exists, err := models.PeerIDExists(database, 100)
	assert.NoError(t, err)
	assert.False(t, exists)
}

func TestDeletePeerNonExistentNoError(t *testing.T) {
	t.Parallel()
	database, cleanup := makeTestDB(t)
	defer cleanup()

	// Deleting a non-existent peer should not error in GORM
	// (DELETE with no matching rows is not an error)
	err := models.DeletePeer(database, 999)
	assert.NoError(t, err)
}

func FuzzPeerUnmarshalMsg(f *testing.F) {
	// Seed with a valid msgp-encoded Peer
	goodPeer := models.Peer{
		ID:   12345,
		IP:   "192.168.1.100",
		Port: 62035,
	}
	encoded, err := goodPeer.MarshalMsg(nil)
	if err != nil {
		f.Fatal(err)
	}
	f.Add(encoded)
	f.Add([]byte{})                             // empty
	f.Add([]byte{0xFF, 0xFF, 0xFF, 0xFF, 0xFF}) // garbage
	f.Add([]byte{0x80})                         // truncated msgp map header

	f.Fuzz(func(t *testing.T, data []byte) {
		t.Parallel()
		var p models.Peer
		_, _ = p.UnmarshalMsg(data)
	})
}

// --- Benchmarks ---

// BenchmarkFindPeerByID measures the cost of looking up a single peer by ID.
func BenchmarkFindPeerByID(b *testing.B) {
	database, cleanup := benchMakeTestDB(b)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "benchuser"}
	database.Create(&owner)
	database.Create(&models.Peer{ID: 500, OwnerID: 1, IP: "10.0.0.1", Port: 62035})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.FindPeerByID(database, 500)
	}
}

// BenchmarkPeerIDExists measures the cost of checking peer existence.
func BenchmarkPeerIDExists(b *testing.B) {
	database, cleanup := benchMakeTestDB(b)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "benchuser"}
	database.Create(&owner)
	database.Create(&models.Peer{ID: 500, OwnerID: 1})

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.PeerIDExists(database, 500)
	}
}

// BenchmarkListPeers measures the cost of listing all peers (10 peers).
func BenchmarkListPeers(b *testing.B) {
	database, cleanup := benchMakeTestDB(b)
	defer cleanup()

	owner := models.User{ID: 1, Callsign: "N0CALL", Username: "benchuser"}
	database.Create(&owner)
	for id := uint(100); id < 110; id++ {
		database.Create(&models.Peer{ID: id, OwnerID: 1, IP: "10.0.0.1", Port: 62035})
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.ListPeers(database)
	}
}

// BenchmarkPeerMarshalMsg measures msgp serialization of a Peer with IP/Port.
func BenchmarkPeerMarshalMsg(b *testing.B) {
	p := models.Peer{
		ID:   12345,
		IP:   "192.168.1.100",
		Port: 62035,
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = p.MarshalMsg(nil)
	}
}

// BenchmarkPeerUnmarshalMsg measures msgp deserialization of a Peer with IP/Port.
func BenchmarkPeerUnmarshalMsg(b *testing.B) {
	p := models.Peer{
		ID:   12345,
		IP:   "192.168.1.100",
		Port: 62035,
	}
	encoded, err := p.MarshalMsg(nil)
	if err != nil {
		b.Fatal(err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		var dec models.Peer
		_, _ = dec.UnmarshalMsg(encoded)
	}
}

func benchMakeTestDB(b *testing.B) (*gorm.DB, func()) {
	b.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	if err != nil {
		b.Fatal(err)
	}
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}

	database, err := db.MakeDB(&defConfig)
	if err != nil {
		b.Fatal(err)
	}

	cleanup := func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}
	return database, cleanup
}
