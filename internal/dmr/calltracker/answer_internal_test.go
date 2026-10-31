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
	"encoding/json"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTheMatchOfAPairStaysBounded gives one pair of radios more answers
// than a match keeps, each of a stream of its own and with no row to go
// to, and another pair one answer whose bursts repeat. The match must keep
// matchCap answers for the first pair and one for the second.
func TestTheMatchOfAPairStaysBounded(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.TODO(), &cfg)
	require.NoError(t, err)
	ct := NewCallTracker(database, ps, store)
	t.Cleanup(func() {
		ct.Stop()
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = store.Close()
	})
	read := func(k pair) matchState {
		b, err := store.Get(context.Background(), matchKey(k, answerData))
		require.NoError(t, err)
		var st matchState
		require.NoError(t, json.Unmarshal(b, &st))
		return st
	}

	at := ct.clock.Now()
	for i := range matchCap + 1 {
		ct.answer(0, 1, 2, uint(i), models.CallResponseACK, answerData, at.Add(time.Duration(i)*time.Microsecond), 0, true)
	}
	assert.Len(t, read(pair{src: 2, dst: 1}).Answers, matchCap)

	for i := range 100 {
		ct.answer(0, 3, 4, 77, models.CallResponseACK, answerData, at.Add(time.Duration(i)*time.Millisecond), 0, true)
	}
	assert.Len(t, read(pair{src: 4, dst: 3}).Answers, 1, "each repeated burst of one answer stayed in the match")
}
