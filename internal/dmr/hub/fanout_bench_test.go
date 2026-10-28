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

package hub_test

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func countQueries(tb testing.TB, database *gorm.DB) *atomic.Int64 {
	tb.Helper()
	var n atomic.Int64
	count := func(*gorm.DB) { n.Add(1) }
	cb := database.Callback()
	require.NoError(tb, cb.Query().After("gorm:query").Register("test:count_query", count))
	require.NoError(tb, cb.Row().After("gorm:row").Register("test:count_row", count))
	require.NoError(tb, cb.Raw().After("gorm:raw").Register("test:count_raw", count))
	require.NoError(tb, cb.Create().After("gorm:create").Register("test:count_create", count))
	require.NoError(tb, cb.Update().After("gorm:update").Register("test:count_update", count))
	require.NoError(tb, cb.Delete().After("gorm:delete").Register("test:count_delete", count))
	return &n
}

type fanout struct {
	h        *hub.Hub
	db       *gorm.DB
	handle   *hub.ServerHandle
	queries  *atomic.Int64
	sourceID uint
	targets  int
	seq      uint
}

const fanoutTG = uint(91)

func setupFanout(tb testing.TB, targets int) *fanout {
	tb.Helper()
	h, database, _ := makeTestHubWithPubSub(tb)
	database.Logger = logger.Discard
	seedUser(tb, database, 1000001, "TESTUSER")
	seedTalkgroup(tb, database, fanoutTG, "WW")
	const sourceID = uint(310000)
	seedRepeater(tb, database, sourceID, 1000001)
	handle := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	tb.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	for i := range targets {
		id := uint(311000 + i)
		seedRepeater(tb, database, id, 1000001)
		rpt := models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: id}}
		require.NoError(tb, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: fanoutTG}))
		h.ActivateRepeater(context.Background(), id)
	}
	knowTalkgroups(tb, h)
	f := &fanout{h: h, db: database, handle: handle, queries: countQueries(tb, database), sourceID: sourceID, targets: targets}
	f.burst(tb)
	return f
}

func (f *fanout) burst(tb testing.TB) {
	tb.Helper()
	pkt := makeVoicePacket(fanoutTG, 777, true, false)
	pkt.Repeater = f.sourceID
	pkt.Seq = f.seq % 256
	f.seq++
	f.h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	for got := 0; got < f.targets; got++ {
		select {
		case rp := <-f.handle.Packets:
			if rp.Packet.Dst != fanoutTG {
				tb.Fatalf("delivered dst %d", rp.Packet.Dst)
			}
		case <-time.After(5 * time.Second):
			tb.Fatalf("burst %d: %d of %d deliveries", f.seq, got, f.targets)
		}
	}
}

func BenchmarkGroupBurstFanout(b *testing.B) {
	for _, targets := range []int{1, 10, 50} {
		b.Run(fmt.Sprintf("repeaters=%d", targets), func(b *testing.B) {
			f := setupFanout(b, targets)
			start := f.queries.Load()
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				f.burst(b)
			}
			b.StopTimer()
			b.ReportMetric(float64(f.queries.Load()-start)/float64(b.N), "queries/burst")
		})
	}
}

func TestGroupBurstQueriesDoNotGrowWithRepeaters(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		f := setupFanout(t, 10)
		const bursts = 20
		start := f.queries.Load()
		for range bursts {
			f.burst(t)
		}
		perBurst := float64(f.queries.Load()-start) / bursts
		require.LessOrEqual(t, perBurst, 1.5, "queries per burst to 10 repeaters")
	})
}
