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

package calltracker

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTrackNeverBlocksOnAFullQueue(t *testing.T) {
	t.Parallel()
	defConfig, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	defConfig.Database.Database = fmt.Sprintf("file:memdb_ctq_%p", t)
	defConfig.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&defConfig)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &defConfig)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.TODO(), &defConfig)
	require.NoError(t, err)
	ct := NewCallTracker(database, ps, kvStore)
	release := make(chan struct{})
	t.Cleanup(func() {
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = kvStore.Close()
	})

	p := models.Packet{Src: 3191868, Dst: 42, Repeater: 100001, GroupCall: true, StreamID: 1, FrameType: dmrconst.FrameVoice}
	idx := chanKey{server: models.RepeaterTypeMMDVM, repeater: p.Repeater, slot: p.Slot}.shard()
	ct.shards[idx].in <- task{fn: func(*shard) { <-release }}

	start := time.Now()
	for range shardDepth * 2 {
		ct.Track(context.Background(), p, models.RepeaterTypeMMDVM, false)
	}
	assert.Less(t, time.Since(start), time.Second, "Track must not wait for a busy worker")
	assert.GreaterOrEqual(t, ct.Dropped(), uint64(shardDepth-1))
	close(release)
}
