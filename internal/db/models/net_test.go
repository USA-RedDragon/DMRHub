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
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/configulator"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// closedDB returns an in-memory GORM DB with the underlying connection closed,
// so that every operation returns an error.
func closedDB(t *testing.T) *gorm.DB {
	t.Helper()
	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	sqlDB, err := database.DB()
	require.NoError(t, err)
	require.NoError(t, sqlDB.Close())
	return database
}

// --- Net model error propagation tests ---

func TestFindActiveNetForTalkgroupReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	_, err := models.FindActiveNetForTalkgroup(database, 1)
	assert.Error(t, err)
}

func TestFindNetByIDReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	_, err := models.FindNetByID(database, 1)
	assert.Error(t, err)
}

func TestFindNetsForTalkgroupReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.FindNetsForTalkgroup(database, 1)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestCountNetsForTalkgroupReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	count, err := models.CountNetsForTalkgroup(database, 1)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

func TestListNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.ListNets(database)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestCountNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	count, err := models.CountNets(database)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

func TestListActiveNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.ListActiveNets(database)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestCountActiveNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	count, err := models.CountActiveNets(database)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	net := &models.Net{TalkgroupID: 1, Active: true}
	err := models.CreateNet(database, net)
	assert.Error(t, err)
}

func TestEndNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	err := models.EndNet(database, 1)
	assert.Error(t, err)
}

func TestDeleteNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	err := models.DeleteNet(database, 1)
	assert.Error(t, err)
}

// --- ScheduledNet model error propagation tests ---

func TestFindScheduledNetByIDReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	_, err := models.FindScheduledNetByID(database, 1)
	assert.Error(t, err)
}

func TestFindScheduledNetsForTalkgroupReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.FindScheduledNetsForTalkgroup(database, 1)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestCountScheduledNetsForTalkgroupReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	count, err := models.CountScheduledNetsForTalkgroup(database, 1)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

func TestFindAllEnabledScheduledNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.FindAllEnabledScheduledNets(database)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestListScheduledNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	nets, err := models.ListScheduledNets(database)
	assert.Error(t, err)
	assert.Nil(t, nets)
}

func TestCountScheduledNetsReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	count, err := models.CountScheduledNets(database)
	assert.Error(t, err)
	assert.Equal(t, 0, count)
}

func TestCreateScheduledNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	sn := &models.ScheduledNet{TalkgroupID: 1, Name: "test"}
	err := models.CreateScheduledNet(database, sn)
	assert.Error(t, err)
}

func TestUpdateScheduledNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	sn := &models.ScheduledNet{TalkgroupID: 1, Name: "test"}
	err := models.UpdateScheduledNet(database, sn)
	assert.Error(t, err)
}

func TestDeleteScheduledNetReturnsError(t *testing.T) {
	t.Parallel()
	database := closedDB(t)

	err := models.DeleteScheduledNet(database, 1)
	assert.Error(t, err)
}

// --- GenerateCronExpression unit tests ---

func TestGenerateCronExpressionValid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dayOfWeek int
		timeOfDay string
		expected  string
	}{
		{"Sunday midnight", 0, "00:00", "0 0 * * 0"},
		{"Monday 8am", 1, "08:00", "0 8 * * 1"},
		{"Wednesday noon", 3, "12:00", "0 12 * * 3"},
		{"Friday 9:30pm", 5, "21:30", "30 21 * * 5"},
		{"Saturday 11:59pm", 6, "23:59", "59 23 * * 6"},
		{"Tuesday 5:05am", 2, "05:05", "5 5 * * 2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			cron, err := models.GenerateCronExpression(tt.dayOfWeek, tt.timeOfDay)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cron)
		})
	}
}

func TestGenerateCronExpressionInvalid(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		dayOfWeek int
		timeOfDay string
	}{
		{"negative day", -1, "12:00"},
		{"day too high", 7, "12:00"},
		{"empty time", 0, ""},
		{"invalid time format", 0, "abc"},
		{"hour too high", 0, "24:00"},
		{"negative hour", 0, "-1:00"},
		{"minute too high", 0, "12:60"},
		{"negative minute", 0, "12:-1"},
		{"no colon", 0, "1200"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := models.GenerateCronExpression(tt.dayOfWeek, tt.timeOfDay)
			assert.Error(t, err)
		})
	}
}

// --- Fuzz test for GenerateCronExpression ---

func FuzzGenerateCronExpression(f *testing.F) {
	// Seed corpus with valid and boundary values
	f.Add(0, "00:00")
	f.Add(6, "23:59")
	f.Add(3, "12:30")
	f.Add(-1, "12:00")
	f.Add(7, "12:00")
	f.Add(0, "")
	f.Add(0, "abc")
	f.Add(0, "24:00")
	f.Add(0, "12:60")
	f.Add(100, "99:99")

	f.Fuzz(func(t *testing.T, dayOfWeek int, timeOfDay string) {
		cron, err := models.GenerateCronExpression(dayOfWeek, timeOfDay)
		if err != nil {
			// Error case — cron should be empty
			assert.Empty(t, cron)
			return
		}
		// Success case — cron should not be empty and should not panic
		assert.NotEmpty(t, cron)
	})
}

// --- openDB returns a working in-memory GORM DB for fuzz/benchmark tests ---

func openDB(t *testing.T) (*gorm.DB, func()) {
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

func benchOpenDB(b *testing.B) (*gorm.DB, func()) {
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

// seedCallsForBenchmark creates the given number of calls for a talkgroup
// within a 1-hour window starting at baseTime.
func seedCallsForBenchmark(b *testing.B, database *gorm.DB, tgID uint, count int, baseTime time.Time) {
	b.Helper()
	// Seed owner, repeater, and talkgroup
	database.Create(&models.User{ID: 1, Callsign: "N0CALL", Username: "benchuser"})
	database.Create(&models.Talkgroup{ID: tgID, Name: "BenchTG"})

	toTgID := tgID
	for i := 0; i < count; i++ {
		call := models.Call{
			StreamID:      uint(i + 1),
			StartTime:     baseTime.Add(time.Duration(i) * time.Second),
			Active:        false,
			UserID:        1,
			RepeaterID:    0,
			GroupCall:     true,
			DestinationID: tgID,
			TimeSlot:      false,
			IsToTalkgroup: true,
			ToTalkgroupID: &toTgID,
			Duration:      2 * time.Second,
		}
		database.Create(&call)
	}
}

// --- Fuzz test for FindTalkgroupCallsInTimeRange ---

func FuzzNetTimeRange(f *testing.F) {
	// Seed with various int64 values representing unix timestamps
	f.Add(int64(0), int64(1000000000))
	f.Add(int64(-1), int64(0))
	f.Add(int64(1700000000), int64(1700003600))
	f.Add(int64(1700003600), int64(1700000000))     // inverted range
	f.Add(int64(-62135596800), int64(253402300799)) // min/max representable
	f.Add(int64(0), int64(0))                       // zero-width range

	f.Fuzz(func(t *testing.T, startUnix, endUnix int64) {
		// Clamp to avoid time.Unix panics on extreme values
		const maxUnix = int64(253402300799) // 9999-12-31
		const minUnix = int64(-62135596800) // 0001-01-01
		if startUnix < minUnix {
			startUnix = minUnix
		}
		if startUnix > maxUnix {
			startUnix = maxUnix
		}
		if endUnix < minUnix {
			endUnix = minUnix
		}
		if endUnix > maxUnix {
			endUnix = maxUnix
		}

		startTime := time.Unix(startUnix, 0)
		endTime := time.Unix(endUnix, 0)

		database, cleanup := openDB(t)
		defer cleanup()

		// These should never panic regardless of input
		calls, err := models.FindTalkgroupCallsInTimeRange(database, 1, startTime, endTime)
		assert.NoError(t, err)
		assert.NotNil(t, calls)

		count, err := models.CountTalkgroupCallsInTimeRange(database, 1, startTime, endTime)
		assert.NoError(t, err)
		assert.Equal(t, len(calls), count)
	})
}

// --- Fuzz test for rapid net start/stop ---

func FuzzNetStartStop(f *testing.F) {
	// Seed with various talkgroup IDs and iteration counts
	f.Add(uint(1), uint(1))
	f.Add(uint(1), uint(5))
	f.Add(uint(100), uint(3))
	f.Add(uint(0), uint(1))

	f.Fuzz(func(t *testing.T, tgID, iterations uint) {
		// Bound iterations to prevent extremely long fuzz runs
		if iterations > 20 {
			iterations = 20
		}
		if tgID == 0 {
			tgID = 1
		}

		database, cleanup := openDB(t)
		defer cleanup()

		// Create the talkgroup and user
		database.Create(&models.Talkgroup{ID: tgID, Name: "FuzzTG"})
		database.Create(&models.User{ID: 1, Callsign: "N0CALL", Username: "fuzzuser"})

		for i := uint(0); i < iterations; i++ {
			// Start net — should succeed (no active net)
			net := models.Net{
				TalkgroupID:     tgID,
				StartedByUserID: 1,
				StartTime:       time.Now(),
				Active:          true,
				Description:     fmt.Sprintf("fuzz iteration %d", i),
			}
			err := models.CreateNet(database, &net)
			assert.NoError(t, err)
			assert.NotZero(t, net.ID)

			// Verify it's findable
			found, err := models.FindActiveNetForTalkgroup(database, tgID)
			assert.NoError(t, err)
			assert.Equal(t, net.ID, found.ID)
			assert.True(t, found.Active)

			// End net — should succeed
			err = models.EndNet(database, net.ID)
			assert.NoError(t, err)

			// Verify no active net remains
			_, err = models.FindActiveNetForTalkgroup(database, tgID)
			assert.Error(t, err)
		}
	})
}

// --- Benchmark: FindTalkgroupCallsInTimeRange with varying call counts ---

func BenchmarkFindTalkgroupCallsInTimeRange100(b *testing.B) {
	benchFindTalkgroupCallsInTimeRange(b, 100)
}

func BenchmarkFindTalkgroupCallsInTimeRange1K(b *testing.B) {
	benchFindTalkgroupCallsInTimeRange(b, 1000)
}

func BenchmarkFindTalkgroupCallsInTimeRange10K(b *testing.B) {
	benchFindTalkgroupCallsInTimeRange(b, 10000)
}

func benchFindTalkgroupCallsInTimeRange(b *testing.B, callCount int) {
	database, cleanup := benchOpenDB(b)
	defer cleanup()

	baseTime := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	endTime := baseTime.Add(2 * time.Hour)
	seedCallsForBenchmark(b, database, 1, callCount, baseTime)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.FindTalkgroupCallsInTimeRange(database, 1, baseTime, endTime)
	}
}

// --- Benchmark: ActiveNetLookup (simulates the hot-path xsync.Map lookup in CallTracker) ---

func BenchmarkActiveNetLookup(b *testing.B) {
	// Simulate the CallTracker's activeNets map with varying numbers of active nets.
	activeNets := xsync.NewMap[uint, uint]()
	// Pre-populate with 100 active nets to simulate realistic load.
	for i := uint(1); i <= 100; i++ {
		activeNets.Store(i, i*10)
	}

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Alternate between hits and misses
		tgID := uint(i%200) + 1
		_, _ = activeNets.Load(tgID)
	}
}

func BenchmarkActiveNetLookupParallel(b *testing.B) {
	activeNets := xsync.NewMap[uint, uint]()
	for i := uint(1); i <= 100; i++ {
		activeNets.Store(i, i*10)
	}

	b.ReportAllocs()
	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		i := uint(0)
		for pb.Next() {
			tgID := i%200 + 1
			_, _ = activeNets.Load(tgID)
			i++
		}
	})
}

// BenchmarkActiveNetLookupWithStoreContention measures lookup performance under
// concurrent write contention, simulating net start/stop events happening while
// EndCall is checking for active nets.
func BenchmarkActiveNetLookupWithStoreContention(b *testing.B) {
	activeNets := xsync.NewMap[uint, uint]()
	for i := uint(1); i <= 100; i++ {
		activeNets.Store(i, i*10)
	}

	// Background goroutine doing writes to simulate contention
	done := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		i := uint(0)
		for {
			select {
			case <-done:
				return
			default:
				tgID := i%200 + 1
				if i%2 == 0 {
					activeNets.Store(tgID, tgID*10)
				} else {
					activeNets.Delete(tgID)
				}
				i++
			}
		}
	}()

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		tgID := uint(i%200) + 1
		_, _ = activeNets.Load(tgID)
	}
	b.StopTimer()
	close(done)
	wg.Wait()
}

// --- Benchmark: CronGeneration ---

func BenchmarkCronGeneration(b *testing.B) {
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = models.GenerateCronExpression(i%7, fmt.Sprintf("%02d:%02d", i%24, i%60))
	}
}
