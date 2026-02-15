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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/require"
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

func TestInstanceRegistryNoOtherInstances(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kv, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

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

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kv, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

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

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kv, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

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
