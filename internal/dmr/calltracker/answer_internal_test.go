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
	"math/rand/v2"
	"strconv"
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

// naiveAssign is assign as one loop over the rows for each answer, which
// assign must agree with.
func naiveAssign(st *matchState, heardFirst bool) assignment {
	a := assignment{winner: map[uint]uint{}, resp: map[uint]models.CallResponse{}}
	at := map[uint]time.Time{}
	for _, r := range st.Rows {
		a.resp[r.ID] = models.CallResponseNone
	}
	for _, ans := range st.Answers {
		var best *matchRow
		for i := range st.Rows {
			r := &st.Rows[i]
			if inWindow(r.Start, ans.At) && (best == nil || r.Start.After(best.Start) || (r.Start.Equal(best.Start) && r.ID > best.ID)) {
				best = r
			}
		}
		if best == nil && heardFirst {
			for i := range st.Rows {
				r := &st.Rows[i]
				if r.Start.After(ans.At) && r.Start.Before(ans.At.Add(responseWindow)) && (best == nil || r.Start.Before(best.Start) || (r.Start.Equal(best.Start) && r.ID < best.ID)) {
					best = r
				}
			}
		}
		if best == nil {
			continue
		}
		a.winner[ans.Stream] = best.ID
		if t, ok := at[best.ID]; !ok || ans.At.After(t) {
			at[best.ID], a.resp[best.ID] = ans.At, ans.Response
		}
	}
	for _, r := range st.Rows {
		if !st.knows(r.Start) {
			a.resp[r.ID] = r.Resp
		}
	}
	return a
}

// TestAssignAgreesWithTheNaiveMatch compares assign with naiveAssign on
// random matches with ties, both kinds of answers and pruned answers.
func TestAssignAgreesWithTheNaiveMatch(t *testing.T) {
	t.Parallel()
	rnd := rand.New(rand.NewPCG(1, 2))
	base := time.Unix(1_000_000, 0)
	at := func() time.Time { return base.Add(time.Duration(rnd.IntN(60)) * 500 * time.Millisecond) }
	for range 2000 {
		var st matchState
		for i := range rnd.IntN(12) {
			st.Rows = append(st.Rows, matchRow{ID: uint(rnd.IntN(40) + 1 + i*40), Start: at(), Resp: models.CallResponse([]string{"", "ack", "nack"}[rnd.IntN(3)])})
		}
		for i := range rnd.IntN(12) {
			st.Answers = append(st.Answers, matchAnswer{Stream: uint(i + 1), At: at(), Response: models.CallResponse([]string{"ack", "nack"}[rnd.IntN(2)])})
		}
		if rnd.IntN(3) == 0 {
			st.Cut = at()
		}
		for _, hf := range []bool{false, true} {
			want := naiveAssign(&st, hf)
			got := st.assign(hf)
			require.Equal(t, want.winner, got.winner, "winners of %+v", st)
			require.Equal(t, want.resp, got.resp, "responses of %+v", st)
		}
	}
}

func BenchmarkMatchStep(b *testing.B) {
	for _, n := range []int{1, 64, matchCap} {
		b.Run(strconv.Itoa(n), func(b *testing.B) {
			cfg, err := configulator.New(config.ConfigSchema()).Default()
			require.NoError(b, err)
			store, err := kv.MakeKV(context.TODO(), &cfg)
			require.NoError(b, err)
			b.Cleanup(func() { _ = store.Close() })
			c := &CallTracker{kv: store}
			k := pair{src: 1, dst: 2}
			base := time.Now()
			for i := range n {
				t := base.Add(time.Duration(i) * 50 * time.Millisecond)
				_, err := c.step(k, answerData, matchOp{row: &matchRow{ID: uint(i + 1), Start: t}})
				require.NoError(b, err)
				_, err = c.step(k, answerData, matchOp{answer: &matchAnswer{Stream: uint(i + 1), At: t.Add(time.Millisecond), Response: models.CallResponseACK}, keep: true})
				require.NoError(b, err)
			}
			last := base.Add(time.Duration(n) * 50 * time.Millisecond)
			b.ResetTimer()
			for i := range b.N {
				_, err := c.step(k, answerData, matchOp{answer: &matchAnswer{Stream: 1_000_000, At: last.Add(time.Duration(i%2) * time.Microsecond), Response: models.CallResponseACK}, keep: true})
				require.NoError(b, err)
			}
		})
	}
}
