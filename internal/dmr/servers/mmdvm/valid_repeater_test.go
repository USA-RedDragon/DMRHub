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

package mmdvm

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/puzpuzpuz/xsync/v4"
)

// mockKV is a minimal in-process KV for testing without config or metrics dependencies.
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
		return nil, fmt.Errorf("key %s not found", key)
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

func (m *mockKV) Scan(_ context.Context, _ uint64, _ string, _ int64) ([]string, uint64, error) {
	return nil, 0, nil
}

func (m *mockKV) Close() error {
	return nil
}

// Compile-time check that mockKV implements kv.KV.
var _ kv.KV = (*mockKV)(nil)

// makeTestServer creates a minimal Server with only the kvClient set, sufficient for validRepeater tests.
func makeTestServer(kvStore kv.KV) Server {
	return Server{
		kvClient:  servers.MakeKVClient(kvStore),
		connected: xsync.NewMap[uint, struct{}](),
	}
}

func TestValidRepeater_NonExistent(t *testing.T) {
	t.Parallel()

	s := makeTestServer(newMockKV())
	// Repeater 12345 does not exist in KV — should return false immediately
	if s.validRepeater(context.Background(), 12345, models.RepeaterStateConnected) {
		t.Error("validRepeater should return false for a non-existent repeater")
	}
}

func TestValidRepeater_WrongState(t *testing.T) {
	t.Parallel()

	kvStore := newMockKV()
	kvClient := servers.MakeKVClient(kvStore)
	s := makeTestServer(kvStore)

	// Store a repeater in CHALLENGE_SENT state
	repeater := models.Repeater{Connection: models.RepeaterStateChallengeSent}
	kvClient.StoreRepeater(context.Background(), 12345, repeater)

	// Asking for CONNECTED should fail
	if s.validRepeater(context.Background(), 12345, models.RepeaterStateConnected) {
		t.Error("validRepeater should return false when repeater state does not match")
	}
}

func TestValidRepeater_CorrectState(t *testing.T) {
	t.Parallel()

	kvStore := newMockKV()
	kvClient := servers.MakeKVClient(kvStore)
	s := makeTestServer(kvStore)

	// Store a repeater in CONNECTED state
	repeater := models.Repeater{Connection: models.RepeaterStateConnected}
	kvClient.StoreRepeater(context.Background(), 12345, repeater)

	if !s.validRepeater(context.Background(), 12345, models.RepeaterStateConnected) {
		t.Error("validRepeater should return true when repeater exists with correct state")
	}
}
