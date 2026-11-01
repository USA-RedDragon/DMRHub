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

package calltracker_test

import (
	"context"
	"errors"
	"slices"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

type armedKV struct {
	kv.KV
	failNext atomic.Int32
	down     atomic.Bool
}

func (f *armedKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	if f.down.Load() || f.failNext.Add(-1) >= 0 {
		return errors.New("the KV store is down")
	}
	f.failNext.Store(0)
	return f.KV.Update(ctx, key, ttl, fn) //nolint:wrapcheck
}

func isMergedSave(db *gorm.DB) bool {
	return db.Statement.Table == "calls" && slices.Contains(db.Statement.Selects, "*")
}

func armedReplicas(t *testing.T) (*calltracker.CallTracker, *calltracker.CallTracker, *gorm.DB, *armedKV) {
	t.Helper()
	database, ps, kvStore := trackerDeps(t)
	shared := &armedKV{KV: kvStore}
	one := calltracker.NewCallTracker(database, ps, shared)
	t.Cleanup(one.Stop)
	two := calltracker.NewCallTracker(database, ps, shared)
	t.Cleanup(two.Stop)
	return one, two, database, shared
}

// TestAFailedEndOfASaveCountsThePartsOnce fails the KV Update that ends
// the merged save of a moved call, right after the save committed, so the
// parts stay in the shared record and are saved again. The database
// stores each part once, so the row counts each burst once.
func TestAFailedEndOfASaveCountsThePartsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, shared := armedReplicas(t)
		var saves atomic.Int32
		require.NoError(t, database.Callback().Update().After("gorm:update").Register("test:arm", func(db *gorm.DB) {
			if isMergedSave(db) && saves.Add(1) == 1 {
				shared.failNext.Store(1)
			}
		}))
		s := newSender(t, one, userA, tgID, true, 9101)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 20 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, uint(14), c.TotalPackets, "packets")
	})
}

// TestAStalledSaverCountsThePartsOnce stalls the worker that saved the
// merged row of a moved call for longer than the save lock, right after
// the save, so the other replica saves the parts again. The database
// stores each part once, so the row counts each burst once.
func TestAStalledSaverCountsThePartsOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database, _ := armedReplicas(t)
		var saves atomic.Int32
		require.NoError(t, database.Callback().Update().After("gorm:commit_or_rollback_transaction").Register("test:stall", func(db *gorm.DB) {
			if isMergedSave(db) && saves.Add(1) == 1 {
				time.Sleep(6 * time.Second)
			}
		}))
		s := newSender(t, one, userA, tgID, true, 9102)
		s.voiceCall(1, false)
		one.Flush()
		s.ct = two
		sendSuperframe(s)
		time.Sleep(150 * time.Millisecond)
		s.send(dmrconst.FrameDataSync, uint(dmrconst.DTypeVoiceTerm), layer2.RawBurst{})
		two.Flush()
		for range 20 {
			time.Sleep(time.Second)
			one.Flush()
			two.Flush()
		}

		c := oneRow(t, one, rows(t, database))
		assert.Equal(t, 14, len(c.CallData)/layer2.RawBurstSize, "bursts")
		assert.Equal(t, uint(14), c.TotalPackets, "packets")
	})
}
