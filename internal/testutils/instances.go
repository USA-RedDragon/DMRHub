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

package testutils

import (
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/stretchr/testify/require"
)

// Register gives the hub h a replica ID and registers it in store, as the
// start of DMRHub does, so that the replicas of a test know who serves
// each repeater. Call it before any server of h starts. The test
// deregisters it at its end.
func Register(t *testing.T, h *hub.Hub, store kv.KV) *servers.InstanceRegistry {
	t.Helper()
	id, err := servers.GenerateInstanceID()
	require.NoError(t, err)
	require.NoError(t, h.SetInstanceID(id))
	r := servers.NewInstanceRegistry(context.Background(), store, id)
	t.Cleanup(func() { r.Deregister(context.Background()) })
	return r
}
