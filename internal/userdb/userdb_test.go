// SPDX-License-Identifier: AGPL-3.0-or-later
// DMRHub - Run a DMR network server in a single binary
// Copyright (C) 2023-2024 Jacob McSwain
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
	"strings"
	"testing"
	"time"

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

func TestUpdate(t *testing.T) {
	t.Parallel()
	retry.Retry(t, 5, time.Millisecond, func(r *retry.R) {
		err := Update()
		if err != nil {
			r.Errorf("Update failed: %v", err)
		}
		date, err := GetDate()
		if err != nil {
			r.Errorf("GetDate failed: %v", err)
		}
		if userDB.builtInDate == date {
			r.Errorf("Update did not update the database")
		}
	})
}

func BenchmarkUserDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := UnpackDB()
		if err != nil {
			b.Errorf("Error unpacking database: %v", err)
			b.Fail()
		}
		userDB.isInited.Store(false)
		userDB.isDone.Store(false)
		userDB.dmrUsers.Store(dmrUserDB{})
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
