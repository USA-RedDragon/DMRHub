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

package queue_test

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/queue"
)

func TestNewQueue(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()
	if q == nil {
		t.Fatal("Expected non-nil queue")
	}
}

func TestPushAndDrain(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	count, err := q.Push("key1", []byte("value1"))
	if err != nil {
		t.Fatalf("Unexpected error on Push: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	count, err = q.Push("key1", []byte("value2"))
	if err != nil {
		t.Fatalf("Unexpected error on Push: %v", err)
	}
	if count != 2 {
		t.Errorf("Expected count 2, got %d", count)
	}

	values := q.Drain("key1")
	if len(values) != 2 {
		t.Fatalf("Expected 2 values, got %d", len(values))
	}
	if string(values[0]) != "value1" {
		t.Errorf("Expected 'value1', got '%s'", string(values[0]))
	}
	if string(values[1]) != "value2" {
		t.Errorf("Expected 'value2', got '%s'", string(values[1]))
	}
}

func TestDrainEmptiesQueue(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	_, _ = q.Push("key1", []byte("value1"))

	// First drain should return the value
	values := q.Drain("key1")
	if len(values) != 1 {
		t.Fatalf("Expected 1 value, got %d", len(values))
	}

	// Second drain should return nil (key deleted)
	values = q.Drain("key1")
	if values != nil {
		t.Errorf("Expected nil after drain, got %v", values)
	}
}

func TestDrainNonexistentKey(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	values := q.Drain("nonexistent")
	if values != nil {
		t.Errorf("Expected nil for nonexistent key, got %v", values)
	}
}

func TestDelete(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	_, _ = q.Push("key1", []byte("value1"))
	_, _ = q.Push("key1", []byte("value2"))

	err := q.Delete("key1")
	if err != nil {
		t.Fatalf("Unexpected error on Delete: %v", err)
	}

	values := q.Drain("key1")
	if values != nil {
		t.Errorf("Expected nil after delete, got %v", values)
	}
}

func TestDeleteNonexistentKey(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	err := q.Delete("nonexistent")
	if err != nil {
		t.Fatalf("Unexpected error deleting nonexistent key: %v", err)
	}
}

func TestMultipleKeys(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	_, _ = q.Push("key1", []byte("a"))
	_, _ = q.Push("key2", []byte("b"))
	_, _ = q.Push("key1", []byte("c"))

	values1 := q.Drain("key1")
	values2 := q.Drain("key2")

	if len(values1) != 2 {
		t.Errorf("Expected 2 values for key1, got %d", len(values1))
	}
	if len(values2) != 1 {
		t.Errorf("Expected 1 value for key2, got %d", len(values2))
	}
}

func TestPushBinaryData(t *testing.T) {
	t.Parallel()
	q := queue.NewQueue()

	data := []byte{0x00, 0xFF, 0xAB, 0xCD}
	_, err := q.Push("binary", data)
	if err != nil {
		t.Fatalf("Unexpected error: %v", err)
	}

	values := q.Drain("binary")
	if len(values) != 1 {
		t.Fatalf("Expected 1 value, got %d", len(values))
	}
	if len(values[0]) != 4 {
		t.Errorf("Expected 4 bytes, got %d", len(values[0]))
	}
	for i, b := range data {
		if values[0][i] != b {
			t.Errorf("Byte %d: expected %x, got %x", i, b, values[0][i])
		}
	}
}
