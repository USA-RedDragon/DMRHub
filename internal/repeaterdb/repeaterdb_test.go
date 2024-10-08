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

package repeaterdb

import (
	"strings"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/testutils/retry"
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

func TestRepeaterdbValidRepeater(t *testing.T) {
	t.Parallel()
	if !ValidRepeaterCallsign(313060, "KP4DJT") {
		t.Error("KP4DJT is not in the database")
	}
	repeater, ok := Get(313060)
	if !ok {
		t.Error("KP4DJT is not in the database")
	}
	if repeater.ID != 313060 {
		t.Errorf("KP4DJT has the wrong ID. Expected %d, got %d", 313060, repeater.ID)
	}
	if !strings.EqualFold(repeater.Callsign, "KP4DJT") {
		t.Errorf("KP4DJT has the wrong callsign. Expected \"%s\", got \"%s\"", "KP4DJT", repeater.Callsign)
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
	if !IsValidRepeaterID(313060) {
		t.Error("Valid repeater ID marked invalid")
	}
}

func TestUpdate(t *testing.T) {
	t.Parallel()
	retry.Retry(t, 5, time.Millisecond, func(r *retry.R) {
		err := Update()
		if err != nil {
			r.Errorf("Update failed: %v", err)
		}
		dbDate, err := GetDate()
		if err != nil {
			r.Errorf("GetDate failed: %v", err)
		}
		if repeaterDB.builtInDate == dbDate {
			r.Errorf("Update did not update the database")
		}
	})
}

func BenchmarkRepeaterDB(b *testing.B) {
	for i := 0; i < b.N; i++ {
		err := UnpackDB()
		if err != nil {
			b.Errorf("UnpackDB failed: %v", err)
			b.Fail()
		}
		repeaterDB.isInited.Store(false)
		repeaterDB.isDone.Store(false)
		repeaterDB.dmrRepeaters.Store(dmrRepeaterDB{})
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
	b.StartTimer()
	for i := 0; i < b.N; i++ {
		ValidRepeaterCallsign(313060, "KP4DJT")
	}
}
