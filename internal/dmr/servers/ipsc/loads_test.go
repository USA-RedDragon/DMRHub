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

package ipsc_test

import (
	"context"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAStopWaitsForALoadOfAKey holds the load of the auth key of a peer
// that a packet of the peer starts in the background once the key that the
// server read is older than lookup.TTL, and stops the server. Stop must
// not return while the load runs, since the load reads the database that
// the caller of Stop closes next.
func TestAStopWaitsForALoadOfAKey(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	stack := testutils.SetupIntegrationStack(t, testutils.ClockOn(fake))
	stack.SeedUser(t, 1000001, "N0CALL")
	stack.SeedIPSCRepeater(t, 200001, 1000001, "0000000000000000000000000000000000003039")
	var holding atomic.Bool
	started := make(chan struct{}, 1)
	release := make(chan struct{})
	t.Cleanup(func() {
		select {
		case <-release:
		default:
			close(release)
		}
	})
	require.NoError(t, stack.DB.Callback().Query().Before("gorm:query").Register("hold the keys", func(tx *gorm.DB) {
		where, ok := tx.Statement.Clauses["WHERE"]
		if !holding.Load() || tx.Statement.Table != "repeaters" || !ok || !strings.Contains(fmt.Sprint(where.Expression), "type = ?") {
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}))
	stack.StartServers(t)
	peer := testutils.NewIPSCClient(200001, "0000000000000000000000000000000000003039")
	require.NoError(t, peer.Connect(stack.IPSCAddr))
	t.Cleanup(peer.Close)
	require.NoError(t, peer.WaitReady(30*time.Second))

	holding.Store(true)
	fake.Advance(lookup.TTL + time.Second)
	require.NoError(t, peer.SendGroupVoice(1000001, 1, false, 1))
	<-started

	stopped := make(chan struct{})
	go func() {
		_ = stack.IPSCServer.Stop(context.Background())
		close(stopped)
	}()
	select {
	case <-stopped:
		require.Fail(t, "Stop returned while a load of a key ran")
	case <-time.After(time.Second):
	}
	close(release)
	<-stopped
}
