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

package testutils_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// admittedOnASlowDatabase routes the first burst of a radio to a stack
// whose routing lets only members transmit, while the database holds the
// lookup of the owner of the radio for lookup.Wait of the hub's clock, and
// reports whether the hub took the burst in. A stack with bounded waits
// must decide before the database answers.
func admittedOnASlowDatabase(t *testing.T, bounded bool) bool {
	t.Helper()
	fake := clock.NewFake(time.Now())
	opts := []testutils.Backend{testutils.ClockOn(fake)}
	if bounded {
		opts = append(opts, testutils.BoundedWaits())
	}
	stack := testutils.SetupIntegrationStack(t, opts...)
	stack.SeedUser(t, 1000001, "N0CALL")
	cfg := stack.Config()
	cfg.DMR.Routing = config.Routing{Transmit: config.TransmitMembers}
	stack.Hub.SetConfig(config.NewCurrent(&cfg))

	started := make(chan struct{}, 1)
	release := make(chan struct{})
	var once sync.Once
	t.Cleanup(func() { once.Do(func() { close(release) }) })
	require.NoError(t, stack.DB.Callback().Query().Before("gorm:query").Register("hold", func(*gorm.DB) {
		select {
		case started <- struct{}{}:
		default:
		}
		<-release
	}))

	admitted := make(chan bool, 1)
	stack.Hub.RouteStream(context.Background(), models.Packet{
		Signature: string(dmrconst.CommandDMRD), Src: 1000001, Dst: 9, Repeater: 999998, GroupCall: true,
		FrameType: dmrconst.FrameVoice, DTypeOrVSeq: dmrconst.VoiceA, StreamID: 4242, BER: -1, RSSI: -1,
	}, models.RepeaterTypeMMDVM, func(ok bool) { admitted <- ok })
	<-started
	fake.Advance(lookup.Wait)
	if bounded {
		return <-admitted
	}
	once.Do(func() { close(release) })
	return <-admitted
}

// TestAStackWaitsOutASlowDatabase checks that the hub of an integration
// stack waits for a database lookup that takes longer than lookup.Wait,
// and that one made with BoundedWaits drops the burst, as in production.
func TestAStackWaitsOutASlowDatabase(t *testing.T) {
	t.Parallel()
	t.Run("default", func(t *testing.T) {
		t.Parallel()
		require.True(t, admittedOnASlowDatabase(t, false), "the hub dropped the burst of a radio whose owner the database was slow to give")
	})
	t.Run("bounded", func(t *testing.T) {
		t.Parallel()
		require.False(t, admittedOnASlowDatabase(t, true), "the hub waited past lookup.Wait")
	})
}
