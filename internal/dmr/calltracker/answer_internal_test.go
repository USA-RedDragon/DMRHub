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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
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
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.TODO(), &cfg, kv.WithoutCallBound())
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
	a := assignment{winner: map[uint]uint{}, resp: map[uint]models.CallResponse{}, from: map[uint]matchAnswer{}}
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
		if f, ok := a.from[best.ID]; !ok || ans.At.After(f.At) {
			a.from[best.ID], a.resp[best.ID] = ans, ans.Response
		}
	}
	st.keepDropped(&a)
	return a
}

// TestAssignAgreesWithTheNaiveMatch compares assign with naiveAssign on
// random matches with ties, both kinds of answers and dropped answers.
func TestAssignAgreesWithTheNaiveMatch(t *testing.T) {
	t.Parallel()
	rnd := rand.New(rand.NewPCG(1, 2))
	base := time.Unix(1_000_000, 0)
	at := func() time.Time { return base.Add(time.Duration(rnd.IntN(60)) * 500 * time.Millisecond) }
	for range 2000 {
		var st matchState
		for i := range rnd.IntN(12) {
			st.Rows = append(st.Rows, matchRow{ID: uint(rnd.IntN(40) + 1 + i*40), Start: at(), Resp: models.CallResponse([]string{"", "ack", "nack"}[rnd.IntN(3)]), RespStream: uint(rnd.IntN(20)), RespAt: at()})
		}
		for i := range rnd.IntN(12) {
			st.Answers = append(st.Answers, matchAnswer{Stream: uint(i + 1), At: at(), Response: models.CallResponse([]string{"ack", "nack"}[rnd.IntN(2)])})
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
			store, err := kv.MakeKV(context.TODO(), &cfg, kv.WithoutCallBound())
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

func memoryStore(t testing.TB) kv.KV {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	store, err := kv.MakeKV(context.TODO(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func mustStep(t *testing.T, c *CallTracker, k pair, op matchOp) stepResult {
	t.Helper()
	r, err := c.step(k, answerData, op)
	require.NoError(t, err)
	return r
}

// TestARowThatKeepsItsResponseLosesAnAnswerThatAnotherRowTakes lets the
// match drop an old answer of a row, so that the row keeps its response,
// and then makes a row that started later but before the answer that gave
// the response. Only the later row may have that answer. (Fourth review,
// R4-2.)
func TestARowThatKeepsItsResponseLosesAnAnswerThatAnotherRowTakes(t *testing.T) {
	t.Parallel()
	c := &CallTracker{kv: memoryStore(t)}
	k := pair{src: 1, dst: 2}
	b := time.Unix(2_000_000, 0)
	s := func(d float64) time.Time { return b.Add(time.Duration(d * float64(time.Second))) }
	mustStep(t, c, k, matchOp{answer: &matchAnswer{Stream: 100, At: s(0), Response: models.CallResponseACK}, keep: true})
	mustStep(t, c, k, matchOp{row: &matchRow{ID: 1, Start: s(9.5)}})
	mustStep(t, c, k, matchOp{answer: &matchAnswer{Stream: 101, At: s(19), Response: models.CallResponseACK}, keep: true})
	mustStep(t, c, k, matchOp{row: &matchRow{ID: 9, Start: s(85)}})
	mustStep(t, c, k, matchOp{row: &matchRow{ID: 2, Start: s(15)}})
	m1, _ := c.matched(k, answerData, 1)
	m2, _ := c.matched(k, answerData, 2)
	assert.Equal(t, models.CallResponseACK, m2, "the later row has no ACK")
	assert.Equal(t, models.CallResponseNone, m1, "one answer gives an ACK to two rows")
}

// TestARowNearDroppedAnswersTakesANewAnswer has the cap drop an answer
// just before a row starts, and then answers the row. The row must have
// the answer. (Fourth review, R4-3.)
func TestARowNearDroppedAnswersTakesANewAnswer(t *testing.T) {
	t.Parallel()
	c := &CallTracker{kv: memoryStore(t)}
	k := pair{src: 1, dst: 2}
	b := time.Unix(3_000_000, 0)
	for i := range matchCap + 1 {
		mustStep(t, c, k, matchOp{answer: &matchAnswer{Stream: uint(1000 + i), At: b.Add(time.Duration(i) * time.Millisecond), Response: models.CallResponseACK}, keep: true})
	}
	mustStep(t, c, k, matchOp{row: &matchRow{ID: 2, Start: b.Add(600 * time.Millisecond)}})
	mustStep(t, c, k, matchOp{answer: &matchAnswer{Stream: 5000, At: b.Add(time.Second), Response: models.CallResponseNACK}, keep: true})
	m2, _ := c.matched(k, answerData, 2)
	assert.Equal(t, models.CallResponseNACK, m2, "the row did not take the newest answer")
}

// TestTheHomesOfRowsEmptyAfterTheMatchExpires finishes a radio check and
// waits past the life of its match. The worker that saved the check must
// be in homes until then, and homes must be empty after.
func TestTheHomesOfRowsEmptyAfterTheMatchExpires(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
		require.NoError(t, err)
		ct := NewCallTracker(database, ps, memoryStore(t))
		t.Cleanup(func() {
			ct.Stop()
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
			_ = ps.Close()
		})
		var req pdu.CSBK
		req.LastBlock = true
		require.NoError(t, req.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: 1, CheckedAddress: 2}))
		raw, err := layer2.BuildBurst(&req, 1, layer2.SyncSourceMS)
		require.NoError(t, err)
		ct.Track(context.Background(), models.Packet{Src: 1, Dst: 2, Repeater: 100001, Slot: true, StreamID: 1, FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(raw.SlotType().DataType), DMRData: raw}, models.RepeaterTypeMMDVM, false)
		time.Sleep(2 * controlHang)
		ct.Flush()
		assert.Equal(t, 1, ct.homes.Size(), "the worker that saved the check is not its home")
		time.Sleep(responseWindow + matchLag + sweepInterval)
		ct.Flush()
		assert.Zero(t, ct.homes.Size(), "homes keeps a row past its match")
	})
}
