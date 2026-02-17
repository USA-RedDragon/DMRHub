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

	result, err := rules.PeerShouldEgress(database, peer, packet)
	assert.NoError(t, err)
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

	result, err := rules.PeerShouldEgress(database, peer, packet)
	assert.NoError(t, err)
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

	result, err := rules.PeerShouldEgress(database, peer, packet)
	assert.NoError(t, err)
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

	result, err := rules.PeerShouldIngress(database, &peer, packet)
	assert.NoError(t, err)
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

	result, err := rules.PeerShouldIngress(database, &peer, packet)
	assert.NoError(t, err)
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

	result, err := rules.PeerShouldIngress(database, &peer, packet)
	assert.NoError(t, err)
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
	resultLow, err := rules.PeerShouldEgress(database, peer, packetLow)
	assert.NoError(t, err)
	assert.True(t, resultLow, "Src at lower bound should match")

	packetHigh := &models.Packet{Src: 200, Dst: 300}
	resultHigh, err := rules.PeerShouldEgress(database, peer, packetHigh)
	assert.NoError(t, err)
	assert.True(t, resultHigh, "Src at upper bound should match")

	packetBelow := &models.Packet{Src: 99, Dst: 300}
	resultBelow, err := rules.PeerShouldEgress(database, peer, packetBelow)
	assert.NoError(t, err)
	assert.False(t, resultBelow, "Src below range should not match")

	packetAbove := &models.Packet{Src: 201, Dst: 300}
	resultAbove, err := rules.PeerShouldEgress(database, peer, packetAbove)
	assert.NoError(t, err)
	assert.False(t, resultAbove, "Src above range should not match")
}

// FuzzPeerRuleEgressMatch fuzzes the egress rule matching logic with arbitrary
// rule ranges and packet source IDs to ensure no panics or incorrect results.
func FuzzPeerRuleEgressMatch(f *testing.F) {
	// DMR IDs are 24-bit (max 16,777,215); cap fuzz inputs at 32-bit to stay
	// within SQLite's int64 column range while still covering edge cases.
	f.Add(uint(100), uint(200), uint(150))        // in range
	f.Add(uint(100), uint(200), uint(99))         // below
	f.Add(uint(100), uint(200), uint(201))        // above
	f.Add(uint(0), uint(0), uint(0))              // zeros
	f.Add(uint(200), uint(100), uint(150))        // inverted range (min > max)
	f.Add(uint(1), uint(16777215), uint(8000000)) // full DMR ID range

	f.Fuzz(func(t *testing.T, ruleMin, ruleMax, src uint) {
		t.Parallel()

		// Cap to uint32 range to avoid SQLite int64 overflow
		const maxID = uint(1<<32 - 1)
		ruleMin %= (maxID + 1)
		ruleMax %= (maxID + 1)
		src %= (maxID + 1)

		database, cleanup := makeTestDB(t)
		defer cleanup()

		peer := models.Peer{ID: 1000, Egress: true}
		database.Create(&peer)

		rule := models.PeerRule{
			PeerID:       1000,
			Direction:    false,
			SubjectIDMin: ruleMin,
			SubjectIDMax: ruleMax,
		}
		database.Create(&rule)

		packet := &models.Packet{Src: src, Dst: 1}
		result, err := rules.PeerShouldEgress(database, peer, packet)
		assert.NoError(t, err)

		// Verify the result matches the expected range check
		expected := ruleMin <= src && ruleMax >= src
		assert.Equal(t, expected, result,
			"ruleMin=%d ruleMax=%d src=%d: expected %v got %v",
			ruleMin, ruleMax, src, expected, result)
	})
}

// FuzzPeerRuleIngressMatch fuzzes the ingress rule matching logic with arbitrary
// rule ranges and packet destination IDs.
func FuzzPeerRuleIngressMatch(f *testing.F) {
	f.Add(uint(100), uint(200), uint(150))        // in range
	f.Add(uint(100), uint(200), uint(99))         // below
	f.Add(uint(100), uint(200), uint(201))        // above
	f.Add(uint(0), uint(0), uint(0))              // zeros
	f.Add(uint(200), uint(100), uint(150))        // inverted range
	f.Add(uint(1), uint(16777215), uint(8000000)) // full DMR ID range

	f.Fuzz(func(t *testing.T, ruleMin, ruleMax, dst uint) {
		t.Parallel()

		// Cap to uint32 range to avoid SQLite int64 overflow
		const maxID = uint(1<<32 - 1)
		ruleMin %= (maxID + 1)
		ruleMax %= (maxID + 1)
		dst %= (maxID + 1)

		database, cleanup := makeTestDB(t)
		defer cleanup()

		peer := models.Peer{ID: 2000, Ingress: true}
		database.Create(&peer)

		rule := models.PeerRule{
			PeerID:       2000,
			Direction:    true,
			SubjectIDMin: ruleMin,
			SubjectIDMax: ruleMax,
		}
		database.Create(&rule)

		packet := &models.Packet{Src: 1, Dst: dst}
		result, err := rules.PeerShouldIngress(database, &peer, packet)
		assert.NoError(t, err)

		expected := ruleMin <= dst && ruleMax >= dst
		assert.Equal(t, expected, result,
			"ruleMin=%d ruleMax=%d dst=%d: expected %v got %v",
			ruleMin, ruleMax, dst, expected, result)
	})
}

// --- Benchmarks ---

// BenchmarkPeerShouldEgress1Rule measures egress evaluation with a single rule.
func BenchmarkPeerShouldEgress1Rule(b *testing.B) {
	benchmarkPeerShouldEgress(b, 1)
}

// BenchmarkPeerShouldEgress10Rules measures egress evaluation with 10 rules.
func BenchmarkPeerShouldEgress10Rules(b *testing.B) {
	benchmarkPeerShouldEgress(b, 10)
}

// BenchmarkPeerShouldEgress100Rules measures egress evaluation with 100 rules.
func BenchmarkPeerShouldEgress100Rules(b *testing.B) {
	benchmarkPeerShouldEgress(b, 100)
}

func benchmarkPeerShouldEgress(b *testing.B, numRules uint) {
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
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	peer := models.Peer{ID: 9000, Egress: true}
	database.Create(&peer)

	for i := uint(0); i < numRules; i++ {
		base := i * 1000
		database.Create(&models.PeerRule{
			PeerID:       9000,
			Direction:    false,
			SubjectIDMin: base,
			SubjectIDMax: base + 999,
		})
	}

	// Packet Src matches the LAST rule, worst case for linear scan
	packet := &models.Packet{Src: (numRules - 1) * 1000, Dst: 1}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rules.PeerShouldEgress(database, peer, packet)
	}
}

// BenchmarkPeerShouldIngress1Rule measures ingress evaluation with a single rule.
func BenchmarkPeerShouldIngress1Rule(b *testing.B) {
	benchmarkPeerShouldIngress(b, 1)
}

// BenchmarkPeerShouldIngress10Rules measures ingress evaluation with 10 rules.
func BenchmarkPeerShouldIngress10Rules(b *testing.B) {
	benchmarkPeerShouldIngress(b, 10)
}

func benchmarkPeerShouldIngress(b *testing.B, numRules uint) {
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
	defer func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
	}()

	peer := models.Peer{ID: 9001, Ingress: true}
	database.Create(&peer)

	for i := uint(0); i < numRules; i++ {
		base := i * 1000
		database.Create(&models.PeerRule{
			PeerID:       9001,
			Direction:    true,
			SubjectIDMin: base,
			SubjectIDMax: base + 999,
		})
	}

	packet := &models.Packet{Src: 1, Dst: (numRules - 1) * 1000}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = rules.PeerShouldIngress(database, &peer, packet)
	}
}
