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

package openbridge_test

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAStopWaitsForALoadOfThePeers holds the load of the peers that a
// packet of a peer starts in the background once the peers that the server
// read are older than lookup.TTL, and stops the server. Stop must not return
// while the load runs, since the load reads the database that the caller
// of Stop closes next.
func TestAStopWaitsForALoadOfThePeers(t *testing.T) {
	t.Parallel()
	fake := clock.NewFake(time.Now())
	stack := testutils.SetupIntegrationStack(t, testutils.ClockOn(fake))
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
	loads := make(chan struct{}, 64)
	require.NoError(t, stack.DB.Callback().Query().Before("gorm:query").Register("hold the peers", func(tx *gorm.DB) {
		if tx.Statement.Table != "peers" || !holding.Load() {
			return
		}
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}))
	require.NoError(t, stack.DB.Callback().Query().After("gorm:query").Register("note the peers", func(tx *gorm.DB) {
		if tx.Statement.Table == "peers" {
			select {
			case loads <- struct{}{}:
			default:
			}
		}
	}))
	stack.StartServers(t)
	peer := testutils.NewOpenBridgeClient(500001, "password")
	require.NoError(t, peer.Connect(stack.OpenBridgeAddr))
	t.Cleanup(peer.Close)
	for len(loads) > 0 {
		<-loads
	}
	require.NoError(t, peer.SendGroupVoice(1000001, 1, 1))
	<-loads

	holding.Store(true)
	fake.Advance(lookup.TTL + time.Second)
	require.NoError(t, peer.SendGroupVoice(1000001, 1, 2))
	<-started

	stopped := make(chan struct{})
	go func() {
		_ = stack.OpenBridgeServer.Stop(context.Background())
		close(stopped)
	}()
	select {
	case <-stopped:
		require.Fail(t, "Stop returned while a load of the peers ran")
	case <-time.After(time.Second):
	}
	close(release)
	<-stopped
}
