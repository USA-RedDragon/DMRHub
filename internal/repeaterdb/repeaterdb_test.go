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

package repeaterdb

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmrdb"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/dbtest"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/retry"
	"github.com/puzpuzpuz/xsync/v4"
)

func TestRepeaterdb(t *testing.T) {
	t.Parallel()
	if Len() == 0 {
		t.Error("dmrRepeaters is empty")
	}
	// Check for an obviously wrong number of IDs.
	// As of writing this test, there are 9200 IDs in the database
	if Len() < 9200 {
		t.Errorf("dmrRepeaters is missing repeaters, found %d repeaters", Len())
	}
}

func firstEmbeddedRepeater(t testing.TB) DMRRepeater {
	t.Helper()
	repeater, err := decodeRepeater(dbtest.FirstEntry(t, comressedDMRRepeatersDB, "rptrs"))
	if err != nil || repeater.ID == 0 || repeater.Callsign == "" || len(repeater.Trustees) == 0 {
		t.Fatalf("cannot decode the first embedded repeater: %+v %v", repeater, err)
	}
	return repeater
}

func TestRepeaterdbValidRepeater(t *testing.T) {
	t.Parallel()
	first := firstEmbeddedRepeater(t)
	if !ValidRepeaterCallsign(first.ID, first.Trustees[0]) {
		t.Errorf("%s is not a trustee of %d in the database", first.Trustees[0], first.ID)
	}
	if ValidRepeaterCallsign(first.ID, "N0CALL") {
		t.Errorf("N0CALL is a trustee of %d in the database", first.ID)
	}
	repeater, ok := Get(first.ID)
	if !ok {
		t.Fatalf("%d is not in the database", first.ID)
	}
	if repeater.ID != first.ID {
		t.Errorf("%s has the wrong ID. Expected %d, got %d", first.Callsign, first.ID, repeater.ID)
	}
	if !strings.EqualFold(repeater.Callsign, first.Callsign) {
		t.Errorf("%d has the wrong callsign. Expected %q, got %q", first.ID, first.Callsign, repeater.Callsign)
	}
}

func TestRepeaterdbInvalidRepeater(t *testing.T) {
	t.Parallel()
	// DMR repeater IDs are 6 digits.
	// 7 digits
	if IsValidRepeaterID(9999999) {
		t.Error("9999999 is not a valid repeater ID")
	}
	// 5 digits
	if IsValidRepeaterID(99999) {
		t.Error("99999 is not a valid repeater ID")
	}
	// 4 digits
	if IsValidRepeaterID(9999) {
		t.Error("9999 is not a valid repeater ID")
	}
	// 3 digits
	if IsValidRepeaterID(999) {
		t.Error("999 is not a valid repeater ID")
	}
	// 2 digits
	if IsValidRepeaterID(99) {
		t.Error("99 is not a valid repeater ID")
	}
	// 1 digit
	if IsValidRepeaterID(9) {
		t.Error("9 is not a valid repeater ID")
	}
	// 0 digits
	if IsValidRepeaterID(0) {
		t.Error("0 is not a valid repeater ID")
	}
	if !IsValidRepeaterID(991360) {
		t.Error("Valid repeater ID marked invalid")
	}
}

// newFixtureDB returns a repeater database, separate from the package one, seeded with the old-format fixture.
func newFixtureDB(t *testing.T) *dmrdb.DB[DMRRepeater] {
	t.Helper()
	db := newDB(dbtest.Compress(t, readFixture(t, "rptrs-old.json")), builtInDateStr)
	if err := db.UnpackDB(); err != nil {
		t.Fatalf("UnpackDB failed: %v", err)
	}
	return db
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("reading fixture: %v", err)
	}
	return data
}

func TestUpdateFromEmbeddedDB(t *testing.T) {
	t.Parallel()
	srv := dbtest.NewServer(t, 0, dbtest.Decompress(t, comressedDMRRepeatersDB))
	db := newFixtureDB(t)
	if err := db.Update(srv.URL); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if db.Len() != Len() {
		t.Errorf("Update loaded %d repeaters, UnpackDB loaded %d", db.Len(), Len())
	}
	first := firstEmbeddedRepeater(t)
	repeater, ok := db.Get(first.ID)
	if !ok || !strings.EqualFold(repeater.Callsign, first.Callsign) || !slices.Equal(repeater.Trustees, first.Trustees) {
		t.Errorf("unexpected repeater %d: %+v", first.ID, repeater)
	}
}

func TestUpdateFixtures(t *testing.T) {
	t.Parallel()
	tests := []struct {
		fixture  string
		count    int
		id       uint
		trustees []string
	}{
		{fixture: "rptrs-current.json", count: 4, id: 990101, trustees: []string{"W0CALL", "K6CALL"}},
		{fixture: "rptrs-current.json", count: 4, id: 990206, trustees: []string{"N9TEST"}},
		{fixture: "rptrs-old.json", count: 2, id: 990001, trustees: []string{"K2CALL"}},
	}
	for _, tt := range tests {
		t.Run(tt.fixture, func(t *testing.T) {
			t.Parallel()
			srv := dbtest.NewServer(t, 0, readFixture(t, tt.fixture))
			db := newFixtureDB(t)
			if err := db.Update(srv.URL); err != nil {
				t.Fatalf("Update failed: %v", err)
			}
			if db.Len() != tt.count {
				t.Errorf("expected %d repeaters, got %d", tt.count, db.Len())
			}
			repeater, ok := db.Get(tt.id)
			if !ok {
				t.Fatalf("%d missing", tt.id)
			}
			if !slices.Equal(repeater.Trustees, tt.trustees) {
				t.Errorf("expected trustees %v, got %v", tt.trustees, repeater.Trustees)
			}
			if repeater.ID != tt.id || repeater.Callsign == "" || repeater.ColorCode == 0 || repeater.Frequency == "" {
				t.Errorf("repeater %d decoded incompletely: %+v", tt.id, repeater)
			}
			date, err := db.GetDate()
			if err != nil || date.Equal(db.GetBuiltInDate()) {
				t.Errorf("Update did not move the database date: %v %v", date, err)
			}
		})
	}
}

func TestUpdateFailures(t *testing.T) {
	t.Parallel()
	current := string(readFixture(t, "rptrs-current.json"))
	tests := []struct {
		name string
		url  func(t *testing.T) string
	}{
		{"server error", func(t *testing.T) string { return dbtest.NewServer(t, 1, []byte(current)).URL }},
		{"truncated body", func(t *testing.T) string { return dbtest.NewTruncatedServer(t, []byte(current)) }},
		{"id becomes a string", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"id": 990101`, `"id": "990101"`, 1))).URL
		}},
		{"id renamed", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.ReplaceAll(current, `"id": `, `"repeater_id": `))).URL
		}},
		{"color code becomes a string", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"color_code": 1`, `"color_code": "1"`, 1))).URL
		}},
		{"trustee becomes an object", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"trustee": ["K2CALL"]`, `"trustee": {"callsign": "K2CALL"}`, 1))).URL
		}},
		{"trustees become objects", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"trustee": ["K2CALL"]`, `"trustee": [{"callsign": "K2CALL"}]`, 1))).URL
		}},
		{"rptrs becomes an object", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(`{"rptrs": {"990001": {"id": 990001}}}`)).URL
		}},
		{"no repeaters", func(t *testing.T) string { return dbtest.NewServer(t, 0, []byte(`{"rptrs": []}`)).URL }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			db := newFixtureDB(t)
			err := db.Update(tt.url(t))
			if !errors.Is(err, dmrdb.ErrUpdateFailed) {
				t.Fatalf("expected ErrUpdateFailed, got %v", err)
			}
			if db.Len() != 2 {
				t.Errorf("failed update replaced the database, it now has %d repeaters", db.Len())
			}
			if _, ok := db.Get(991360); !ok {
				t.Error("failed update dropped repeater 991360")
			}
		})
	}
}

func TestUpdateRetriesWithBackoff(t *testing.T) {
	t.Parallel()
	srv := dbtest.NewServer(t, 2, readFixture(t, "rptrs-current.json"))
	db := newFixtureDB(t)
	var delays []time.Duration
	backoff := retry.Backoff{
		Attempts: 5,
		Initial:  time.Second,
		Max:      4 * time.Second,
		Sleep:    func(d time.Duration) { delays = append(delays, d) },
	}
	ok := backoff.Run(t, func(r *retry.R) {
		if err := db.Update(srv.URL); err != nil {
			r.Errorf("Update failed: %v", err)
		}
	})
	if !ok {
		t.Fatal("Update never succeeded")
	}
	if srv.Hits() != 3 {
		t.Errorf("expected 3 requests, got %d", srv.Hits())
	}
	if want := []time.Duration{time.Second, 2 * time.Second}; !slices.Equal(delays, want) {
		t.Errorf("expected delays %v, got %v", want, delays)
	}
	if db.Len() != 4 {
		t.Errorf("expected 4 repeaters, got %d", db.Len())
	}
}

func TestStreamDecodeRepeaters(t *testing.T) {
	t.Parallel()
	const data = `{"rptrs":[` +
		`{"id":990101,"callsign":"W0CALL","trustee":["W0CALL","K6CALL"],"map_info":null,` +
		`"talkgroups":[{"talkgroup":5152,"description":"Local","timeslot":1,"discovery":0}],"color_code":3},` +
		`{"id":990001,"callsign":"N3TEST","trustee":"K2CALL","lat":"47.2","status":"ACTIVE","color_code":2}` +
		`]}`

	m := xsync.NewMap[uint, DMRRepeater]()
	count, err := streamDecodeRepeaters(json.NewDecoder(strings.NewReader(data)), m)
	if err != nil {
		t.Fatalf("streamDecodeRepeaters failed: %v", err)
	}
	if count != 2 {
		t.Fatalf("expected 2 repeaters, got %d", count)
	}

	multi, ok := m.Load(990101)
	if !ok {
		t.Fatal("990101 missing")
	}
	if !slices.Equal(multi.Trustees, []string{"W0CALL", "K6CALL"}) {
		t.Errorf("unexpected trustees %v", multi.Trustees)
	}
	if multi.ColorCode != 3 {
		t.Errorf("expected color code 3, got %d", multi.ColorCode)
	}

	single, ok := m.Load(990001)
	if !ok {
		t.Fatal("990001 missing")
	}
	if !slices.Equal(single.Trustees, []string{"K2CALL"}) {
		t.Errorf("unexpected trustees %v", single.Trustees)
	}
}

func BenchmarkRepeaterDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := UnpackDB()
		if err != nil {
			b.Errorf("UnpackDB failed: %v", err)
			b.Fail()
		}
		repeaterDB.ResetForBenchmark()
	}
}

func BenchmarkRepeaterSearch(b *testing.B) {
	// The first run will decompress the database, so we'll do that first
	b.StopTimer()
	err := UnpackDB()
	if err != nil {
		b.Errorf("UnpackDB failed: %v", err)
		b.Fail()
	}
	first := firstEmbeddedRepeater(b)
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ValidRepeaterCallsign(first.ID, first.Trustees[0])
	}
}
