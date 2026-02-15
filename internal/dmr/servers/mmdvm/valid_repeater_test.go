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
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/configulator"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/require"
)

// makeTestServer creates a minimal Server with only the kvClient set, sufficient for validRepeater tests.
func makeTestServer(kvStore kv.KV) Server {
	return Server{
		kvClient:  servers.MakeKVClient(kvStore),
		connected: xsync.NewMap[uint, struct{}](),
	}
}

func TestValidRepeater_NonExistent(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	s := makeTestServer(kvStore)
	// Repeater 12345 does not exist in KV — should return false immediately
	if s.validRepeater(context.Background(), 12345, models.RepeaterStateConnected) {
		t.Error("validRepeater should return false for a non-existent repeater")
	}
}

func TestValidRepeater_WrongState(t *testing.T) {
	t.Parallel()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

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

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)

	kvStore, err := kv.MakeKV(context.Background(), &defConfig)
	require.NoError(t, err)

	kvClient := servers.MakeKVClient(kvStore)
	s := makeTestServer(kvStore)

	// Store a repeater in CONNECTED state
	repeater := models.Repeater{Connection: models.RepeaterStateConnected}
	kvClient.StoreRepeater(context.Background(), 12345, repeater)

	if !s.validRepeater(context.Background(), 12345, models.RepeaterStateConnected) {
		t.Error("validRepeater should return true when repeater exists with correct state")
	}
}
