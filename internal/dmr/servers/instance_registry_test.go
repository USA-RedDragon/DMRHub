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

package servers

import (
	"context"
	"testing"
	"time"
)

func TestWithGracefulHandoff(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Default: no graceful handoff
	if IsGracefulHandoff(ctx) {
		t.Fatal("expected IsGracefulHandoff to be false on plain context")
	}

	// Set to true
	ctx = WithGracefulHandoff(ctx, true)
	if !IsGracefulHandoff(ctx) {
		t.Fatal("expected IsGracefulHandoff to be true")
	}

	// Set to false explicitly
	ctx = WithGracefulHandoff(ctx, false)
	if IsGracefulHandoff(ctx) {
		t.Fatal("expected IsGracefulHandoff to be false when set explicitly")
	}
}

func TestGenerateInstanceID(t *testing.T) {
	t.Parallel()

	id1, err := GenerateInstanceID()
	if err != nil {
		t.Fatalf("GenerateInstanceID error: %v", err)
	}
	id2, err := GenerateInstanceID()
	if err != nil {
		t.Fatalf("GenerateInstanceID error: %v", err)
	}

	if id1 == "" {
		t.Fatal("expected non-empty instance ID")
	}
	if id1 == id2 {
		t.Fatalf("expected unique instance IDs, got %q twice", id1)
	}
	// Should be hex-encoded 8 bytes = 16 characters
	if len(id1) != 16 {
		t.Fatalf("expected 16-char instance ID, got %d: %q", len(id1), id1)
	}
}

// mockKV is a minimal in-process KV store for testing.
type mockKV struct {
	data map[string][]byte
}

func newMockKV() *mockKV {
	return &mockKV{data: make(map[string][]byte)}
}

func (m *mockKV) Has(_ context.Context, key string) (bool, error) {
	_, ok := m.data[key]
	return ok, nil
}

func (m *mockKV) Get(_ context.Context, key string) ([]byte, error) {
	v, ok := m.data[key]
	if !ok {
		return nil, ErrNoSuchRepeater
	}
	return v, nil
}

func (m *mockKV) Set(_ context.Context, key string, value []byte) error {
	m.data[key] = value
	return nil
}

func (m *mockKV) Delete(_ context.Context, key string) error {
	delete(m.data, key)
	return nil
}

func (m *mockKV) Expire(_ context.Context, _ string, _ time.Duration) error {
	return nil
}

func (m *mockKV) Scan(_ context.Context, _ uint64, match string, _ int64) ([]string, uint64, error) {
	var keys []string
	for k := range m.data {
		if matchGlob(match, k) {
			keys = append(keys, k)
		}
	}
	return keys, 0, nil
}

func (m *mockKV) Close() error {
	return nil
}

// matchGlob is a very simplistic glob matcher that only supports trailing '*'.
func matchGlob(pattern, s string) bool {
	if pattern == "" {
		return true
	}
	if pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(s) >= len(prefix) && s[:len(prefix)] == prefix
	}
	return pattern == s
}

func TestInstanceRegistryNoOtherInstances(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	ctx := context.Background()

	r := NewInstanceRegistry(ctx, kv, "instance-1")
	defer r.Deregister(ctx)

	// Only this instance is registered
	if r.OtherInstancesExist(ctx) {
		t.Fatal("expected no other instances")
	}
}

func TestInstanceRegistryWithOtherInstances(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	ctx := context.Background()

	r1 := NewInstanceRegistry(ctx, kv, "instance-1")
	defer r1.Deregister(ctx)

	r2 := NewInstanceRegistry(ctx, kv, "instance-2")
	defer r2.Deregister(ctx)

	// Each should see the other
	if !r1.OtherInstancesExist(ctx) {
		t.Fatal("instance-1 should see instance-2")
	}
	if !r2.OtherInstancesExist(ctx) {
		t.Fatal("instance-2 should see instance-1")
	}
}

func TestInstanceRegistryDeregister(t *testing.T) {
	t.Parallel()

	kv := newMockKV()
	ctx := context.Background()

	r1 := NewInstanceRegistry(ctx, kv, "instance-1")
	r2 := NewInstanceRegistry(ctx, kv, "instance-2")

	// Both see each other
	if !r1.OtherInstancesExist(ctx) {
		t.Fatal("instance-1 should see instance-2")
	}

	// Deregister instance-2
	r2.Deregister(ctx)

	// Now instance-1 should be alone
	if r1.OtherInstancesExist(ctx) {
		t.Fatal("instance-1 should not see any other instances after instance-2 deregistered")
	}

	r1.Deregister(ctx)
}
