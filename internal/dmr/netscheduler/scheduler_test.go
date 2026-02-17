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

package netscheduler_test

import (
	"context"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/netscheduler"
	kvPkg "github.com/USA-RedDragon/DMRHub/internal/kv"
	psPkg "github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestStack(t *testing.T) (*netscheduler.NetScheduler, func()) {
	t.Helper()

	defConfig, err := configulator.New[config.Config]().Default()
	require.NoError(t, err)
	defConfig.Database.Database = ""
	defConfig.Database.ExtraParameters = []string{}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)

	ctx := context.Background()
	ps, err := psPkg.MakePubSub(ctx, &defConfig)
	require.NoError(t, err)
	kvStore, err := kvPkg.MakeKV(ctx, &defConfig)
	require.NoError(t, err)

	ns := netscheduler.NewNetScheduler(database, ps, kvStore)
	require.NotNil(t, ns)

	cleanup := func() {
		ns.Stop()
	}
	return ns, cleanup
}

func TestNewNetSchedulerNotNil(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)
	assert.NotNil(t, ns)
}

func TestStartAndStopDoesNotPanic(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)

	ns.Start()
	ns.Stop()
}

func TestLoadScheduledNetsEmpty(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)

	err := ns.LoadScheduledNets(context.Background())
	require.NoError(t, err)
}

func TestScheduleAutoCloseAndCancel(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)
	ns.Start()

	ns.ScheduleAutoClose(999, 10*time.Second)
	ns.CancelAutoClose(999)
}

func TestCancelAutoCloseNonExistent(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)

	ns.CancelAutoClose(12345)
}

func TestUnregisterScheduledNetNonExistent(t *testing.T) {
	t.Parallel()
	ns, cleanup := makeTestStack(t)
	t.Cleanup(cleanup)

	ns.UnregisterScheduledNet(99999)
}
