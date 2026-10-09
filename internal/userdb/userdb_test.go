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

package userdb

import (
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
)

func TestUserdb(t *testing.T) {
	t.Parallel()
	if Len() == 0 {
		t.Error("dmrUsers is empty")
	}
	// Check for an obviously wrong number of IDs.
	// As of writing this test, there are 232,772 IDs in the database
	if Len() < 231290 {
		t.Errorf("dmrUsers is missing users, found %d users", Len())
	}
}

func TestUserdbValidUser(t *testing.T) {
	t.Parallel()
	if !ValidUserCallsign(3191868, "KI5VMF") {
		t.Error("KI5VMF is not in the database")
	}
	me, ok := Get(3191868)
	if !ok {
		t.Error("KI5VMF is not in the database")
	}
	if me.ID != 3191868 {
		t.Errorf("KI5VMF has the wrong ID. Expected %d, got %d", 3191868, me.ID)
	}
	if !strings.EqualFold(me.Callsign, "KI5VMF") {
		t.Errorf("KI5VMF has the wrong callsign. Expected \"%s\", got \"%s\"", "KI5VMF", me.Callsign)
	}
}

func TestUserdbInvalidUser(t *testing.T) {
	t.Parallel()
	// DMR User IDs are 7 digits.
	if IsValidUserID(10000000) {
		t.Error("10000000 is not a valid user ID")
	}
	// 6 digits
	if IsValidUserID(999999) {
		t.Error("999999 is not a valid user ID")
	}
	// 5 digits
	if IsValidUserID(99999) {
		t.Error("99999 is not a valid user ID")
	}
	// 4 digits
	if IsValidUserID(9999) {
		t.Error("9999 is not a valid user ID")
	}
	// 3 digits
	if IsValidUserID(999) {
		t.Error("999 is not a valid user ID")
	}
	// 2 digits
	if IsValidUserID(99) {
		t.Error("99 is not a valid user ID")
	}
	// 1 digit
	if IsValidUserID(9) {
		t.Error("9 is not a valid user ID")
	}
	// 0 digits
	if IsValidUserID(0) {
		t.Error("0 is not a valid user ID")
	}
	if !IsValidUserID(3191868) {
		t.Error("Valid user ID marked invalid")
	}
}

// newFixtureDB returns a user database, separate from the package one, seeded with the old-format fixture.
func newFixtureDB(t *testing.T) *dmrdb.DB[DMRUser] {
	t.Helper()
	db := newDB(dbtest.Compress(t, readFixture(t, "users-old.json")), builtInDateStr)
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
	srv := dbtest.NewServer(t, 0, dbtest.Decompress(t, compressedDMRUsersDB))
	db := newFixtureDB(t)
	if err := db.Update(srv.URL); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if db.Len() != Len() {
		t.Errorf("Update loaded %d users, UnpackDB loaded %d", db.Len(), Len())
	}
	user, ok := db.Get(3191868)
	if !ok || !strings.EqualFold(user.Callsign, "KI5VMF") {
		t.Errorf("unexpected user 3191868: %+v", user)
	}
}

func TestUpdateFixtures(t *testing.T) {
	t.Parallel()
	for _, fixture := range []string{"users-current.json", "users-old.json"} {
		t.Run(fixture, func(t *testing.T) {
			t.Parallel()
			srv := dbtest.NewServer(t, 0, readFixture(t, fixture))
			db := newFixtureDB(t)
			if err := db.Update(srv.URL); err != nil {
				t.Fatalf("Update failed: %v", err)
			}
			if db.Len() != 2 {
				t.Errorf("expected 2 users, got %d", db.Len())
			}
			want := DMRUser{
				ID:       1023007,
				RadioID:  1023007,
				Name:     "Hans Juergen",
				FName:    "Hans Juergen",
				Callsign: "VA3BOC",
				City:     "Cornwall",
				State:    "Ontario",
				Country:  "Canada",
			}
			if user, _ := db.Get(1023007); user != want {
				t.Errorf("expected %+v, got %+v", want, user)
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
	current := string(readFixture(t, "users-current.json"))
	tests := []struct {
		name string
		url  func(t *testing.T) string
	}{
		{"server error", func(t *testing.T) string { return dbtest.NewServer(t, 1, []byte(current)).URL }},
		{"truncated body", func(t *testing.T) string { return dbtest.NewTruncatedServer(t, []byte(current)) }},
		{"id becomes a string", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"id": 1023007`, `"id": "1023007"`, 1))).URL
		}},
		{"id renamed", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.ReplaceAll(current, `"id": `, `"user_id": `))).URL
		}},
		{"callsign becomes an array", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(strings.Replace(current, `"callsign": "VA3BOC"`, `"callsign": ["VA3BOC"]`, 1))).URL
		}},
		{"users becomes an object", func(t *testing.T) string {
			return dbtest.NewServer(t, 0, []byte(`{"users": {"1023007": {"id": 1023007}}}`)).URL
		}},
		{"no users", func(t *testing.T) string { return dbtest.NewServer(t, 0, []byte(`{"users": []}`)).URL }},
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
				t.Errorf("failed update replaced the database, it now has %d users", db.Len())
			}
			if _, ok := db.Get(3191868); !ok {
				t.Error("failed update dropped user 3191868")
			}
		})
	}
}

func TestUpdateIgnoresNewFields(t *testing.T) {
	t.Parallel()
	data := `{"users": [{"id": 3191868, "callsign": "KI5VMF", "flags": {"validated": [1, 2]}, "radio_id": 3191868}]}`
	srv := dbtest.NewServer(t, 0, []byte(data))
	db := newFixtureDB(t)
	if err := db.Update(srv.URL); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if user, ok := db.Get(3191868); !ok || user.Callsign != "KI5VMF" || user.RadioID != 3191868 {
		t.Errorf("unexpected user 3191868: %+v", user)
	}
}

func TestUpdateRetriesWithBackoff(t *testing.T) {
	t.Parallel()
	srv := dbtest.NewServer(t, 2, readFixture(t, "users-current.json"))
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
}

func BenchmarkUserDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := UnpackDB()
		if err != nil {
			b.Errorf("Error unpacking database: %v", err)
			b.Fail()
		}
		userDB.ResetForBenchmark()
	}
}

func BenchmarkUserSearch(b *testing.B) {
	// The first run will decompress the database, so we'll do that first
	b.StopTimer()
	err := UnpackDB()
	if err != nil {
		b.Errorf("Error unpacking database: %v", err)
		b.Fail()
	}
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ValidUserCallsign(3191868, "KI5VMF")
	}
}
