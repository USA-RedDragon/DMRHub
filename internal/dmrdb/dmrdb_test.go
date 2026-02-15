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

package dmrdb

import (
	"bytes"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/puzpuzpuz/xsync/v4"
	"github.com/ulikunitz/xz"
)

type testEntry struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
}

// createCompressedJSON creates an xz-compressed JSON blob with the given entries
// formatted as {"items": [...]}.
func createCompressedJSON(t *testing.T, items []testEntry) []byte {
	t.Helper()

	data := struct {
		Items []testEntry `json:"items"`
	}{Items: items}
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var buf bytes.Buffer
	w, err := xz.NewWriter(&buf)
	if err != nil {
		t.Fatalf("xz.NewWriter: %v", err)
	}
	if _, err := w.Write(jsonBytes); err != nil {
		t.Fatalf("xz write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("xz close: %v", err)
	}
	return buf.Bytes()
}

func testDecoder(dec *json.Decoder, m *xsync.Map[uint, testEntry]) (int, error) {
	t, err := dec.Token()
	if err != nil {
		return 0, ErrDecodingDB
	}
	if delim, ok := t.(json.Delim); !ok || delim != '{' {
		return 0, ErrDecodingDB
	}

	count := 0
	for dec.More() {
		t, err = dec.Token()
		if err != nil {
			return 0, ErrDecodingDB
		}
		key, ok := t.(string)
		if !ok {
			return 0, ErrDecodingDB
		}

		if key == "items" {
			t, err = dec.Token()
			if err != nil {
				return 0, ErrDecodingDB
			}
			if delim, ok := t.(json.Delim); !ok || delim != '[' {
				return 0, ErrDecodingDB
			}

			for dec.More() {
				var entry testEntry
				if err := dec.Decode(&entry); err != nil {
					return 0, ErrDecodingDB
				}
				m.Store(entry.ID, entry)
				count++
			}

			if _, err = dec.Token(); err != nil {
				return 0, ErrDecodingDB
			}
		} else {
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				return 0, ErrDecodingDB
			}
		}
	}

	return count, nil
}

func newTestDB(t *testing.T, items []testEntry) *DB[testEntry] {
	t.Helper()
	dateStr := time.Now().Format(time.RFC3339)
	compressed := createCompressedJSON(t, items)
	return NewDB[testEntry](Config[testEntry]{
		CompressedData: compressed,
		BuiltInDateStr: dateStr,
		Presize:        len(items),
		Decode:         testDecoder,
		EntityName:     "test",
	})
}

func TestUnpackDB(t *testing.T) {
	t.Parallel()
	items := []testEntry{
		{ID: 1, Name: "Alice"},
		{ID: 2, Name: "Bob"},
		{ID: 3, Name: "Charlie"},
	}
	db := newTestDB(t, items)

	err := db.UnpackDB()
	if err != nil {
		t.Fatalf("UnpackDB: %v", err)
	}

	if db.Len() != 3 {
		t.Errorf("expected Len() == 3, got %d", db.Len())
	}
}

func TestGet(t *testing.T) {
	t.Parallel()
	items := []testEntry{
		{ID: 42, Name: "TestUser"},
	}
	db := newTestDB(t, items)

	err := db.UnpackDB()
	if err != nil {
		t.Fatalf("UnpackDB: %v", err)
	}

	entry, ok := db.Get(42)
	if !ok {
		t.Fatal("expected to find entry with ID 42")
	}
	if entry.Name != "TestUser" {
		t.Errorf("expected Name == TestUser, got %s", entry.Name)
	}

	_, ok = db.Get(999)
	if ok {
		t.Error("expected not to find entry with ID 999")
	}
}

func TestGetDate(t *testing.T) {
	t.Parallel()
	items := []testEntry{{ID: 1, Name: "A"}}
	db := newTestDB(t, items)

	err := db.UnpackDB()
	if err != nil {
		t.Fatalf("UnpackDB: %v", err)
	}

	date, err := db.GetDate()
	if err != nil {
		t.Fatalf("GetDate: %v", err)
	}
	if date.IsZero() {
		t.Error("expected non-zero date")
	}
	if !date.Equal(db.GetBuiltInDate()) {
		t.Error("expected GetDate to return built-in date before any update")
	}
}

func TestUnpackDBEmpty(t *testing.T) {
	t.Parallel()
	db := newTestDB(t, []testEntry{})

	err := db.UnpackDB()
	if err == nil {
		t.Fatal("expected error for empty database")
	}
	if !errors.Is(err, ErrNoEntries) {
		t.Errorf("expected ErrNoEntries, got %v", err)
	}
}

func TestUnpackDBBadDate(t *testing.T) {
	t.Parallel()
	compressed := createCompressedJSON(t, []testEntry{{ID: 1, Name: "A"}})
	db := NewDB[testEntry](Config[testEntry]{
		CompressedData: compressed,
		BuiltInDateStr: "not-a-date",
		Presize:        1,
		Decode:         testDecoder,
		EntityName:     "test",
	})

	err := db.UnpackDB()
	if err == nil {
		t.Fatal("expected error for bad date")
	}
	if !errors.Is(err, ErrParsingDate) {
		t.Errorf("expected ErrParsingDate, got %v", err)
	}
}

func TestResetForBenchmark(t *testing.T) {
	t.Parallel()
	items := []testEntry{{ID: 1, Name: "A"}}
	db := newTestDB(t, items)

	err := db.UnpackDB()
	if err != nil {
		t.Fatalf("UnpackDB: %v", err)
	}
	if db.Len() != 1 {
		t.Fatalf("expected Len() == 1, got %d", db.Len())
	}

	db.ResetForBenchmark()

	entry, ok := db.Get(1)
	if !ok {
		t.Fatal("expected to find entry after reset + lazy unpack")
	}
	if entry.Name != "A" {
		t.Errorf("expected Name == A, got %s", entry.Name)
	}
}

func TestConcurrentUnpack(t *testing.T) {
	t.Parallel()
	items := []testEntry{
		{ID: 1, Name: "A"},
		{ID: 2, Name: "B"},
	}
	db := newTestDB(t, items)

	errs := make(chan error, 10)
	for i := 0; i < 10; i++ {
		go func() {
			errs <- db.UnpackDB()
		}()
	}

	for i := 0; i < 10; i++ {
		if err := <-errs; err != nil {
			t.Errorf("concurrent UnpackDB: %v", err)
		}
	}

	if db.Len() != 2 {
		t.Errorf("expected Len() == 2 after concurrent unpack, got %d", db.Len())
	}
}
