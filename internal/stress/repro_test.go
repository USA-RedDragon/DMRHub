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

//go:build stress

package stress_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
)

// TestReproUnregisterServerWithBackedUpEgress is the minimal form of the
// shutdown hang that the call mix on Postgres hit: when the channel from
// the hub to a peer server is full, the peer egress workers block in
// deliverToServer holding the read lock of the server entry, and
// UnregisterServer, which the server's Stop calls before Hub.Stop closes
// done, waits for the write lock forever.
func TestReproUnregisterServerWithBackedUpEgress(t *testing.T) { //nolint:paralleltest
	stack := testutils.SetupIntegrationStack(t)
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedPeer(t, 3100100, 1000001, "x", "127.0.0.1", 1, false, true)
	stack.SeedPeerRule(t, 3100100, false, 1, dmrconst.MaxDMRAddress)
	ct := calltracker.NewCallTracker(stack.DB, stack.PubSub)
	t.Cleanup(ct.Stop)
	h := hub.NewHub(stack.DB, stack.KV, stack.PubSub, ct)
	h.RegisterServer(hub.ServerConfig{Name: "peers", Role: hub.RolePeer})
	h.RegisterServer(hub.ServerConfig{Name: "repeaters", Role: hub.RoleRepeater})

	for i := range 520 {
		h.RoutePacket(context.Background(), models.Packet{
			Signature: string(dmrconst.CommandDMRD), Seq: uint(i & 0xFF), Src: 1000001, Dst: 91, GroupCall: true,
			FrameType: dmrconst.FrameVoice, StreamID: 0xABC, Repeater: 310000,
		}, "repeaters")
	}
	time.Sleep(3 * time.Second)

	done := make(chan struct{})
	go func() {
		h.UnregisterServer("peers")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		h.Stop(context.Background())
		<-done
		require.Fail(t, "UnregisterServer blocked for 5s on a server whose channel from the hub is full; it returned only after Hub.Stop")
	}
	h.Stop(context.Background())
}
