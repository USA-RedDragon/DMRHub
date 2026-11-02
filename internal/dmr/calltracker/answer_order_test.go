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
	"encoding/json"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// workerApart returns a repeater whose slot of TS2 is on a worker apart
// from the workers of the repeaters in not.
func workerApart(not ...uint) uint {
	taken := map[int]bool{}
	for _, r := range not {
		taken[calltracker.WorkerOf(models.RepeaterTypeMMDVM, r, true)] = true
	}
	r := repeaterA + 1
	for taken[calltracker.WorkerOf(models.RepeaterTypeMMDVM, r, true)] {
		r++
	}
	return r
}

func radioCheckBursts(t *testing.T) (req, ans layer2.RawBurst) {
	t.Helper()
	r, a := radioCheckPair(t)
	r.LastBlock, a.LastBlock = true, true
	return burstOf(t, &r), burstOf(t, &a)
}

func confirmedText(t *testing.T, body string) []layer2.RawBurst {
	t.Helper()
	return smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(userB), Destination: uint32(userA), Confirmed: true, Body: []byte(body)})
}

func byStream(calls []models.Call) map[uint]models.Call {
	out := map[uint]models.Call{}
	for _, c := range calls {
		out[c.StreamID] = c
	}
	return out
}

// TestAnAnswerGoesToTheMessageBeforeItWhileItsWorkerLags sends two
// confirmed messages and the ACK of the second from a repeater whose
// worker is another. The worker of the messages is held while the ACK is
// handled, so the row of the second message is made after the ACK. The
// ACK must still end on the second message and not on the first.
func TestAnAnswerGoesToTheMessageBeforeItWhileItsWorkerLags(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		second := confirmedText(t, "two")
		newSender(t, ct, userB, userA, false, 7101).data(confirmedText(t, "one")...)
		ct.Flush()
		time.Sleep(time.Second)

		release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, repeaterA, true)
		newSender(t, ct, userB, userA, false, 7102).data(second...)
		time.Sleep(100 * time.Millisecond)
		ack := newSender(t, ct, userA, userB, false, 7103)
		ack.repeater = workerApart(repeaterA)
		ack.data(confirmedACK(t, second)...)
		synctest.Wait()
		release()

		got := byStream(waitDone(t, ct, database, 3))
		assert.Equal(t, models.CallResponseNone, got[7101].Response, "the ACK of the second message went to the first")
		assert.Equal(t, models.CallResponseACK, got[7102].Response, "the second message has no ACK")
		assert.Equal(t, "response", got[7103].SubKind)
	})
}

// TestARadioCheckAnswerGoesToTheCheckBeforeItWhileItsWorkerLags answers
// one radio check, then sends a second whose worker is held while its
// answer, from a repeater of another worker, is handled. The second answer
// must end on the second check, and neither answer keeps a row.
func TestARadioCheckAnswerGoesToTheCheckBeforeItWhileItsWorkerLags(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		other := workerApart(repeaterA)
		req, ans := radioCheckBursts(t)

		newSender(t, ct, userA, userB, false, 7201).data(req)
		ct.Flush()
		back := newSender(t, ct, userB, userA, false, 7202)
		back.repeater = other
		back.data(ans)
		ct.Flush()
		time.Sleep(2 * time.Second)

		release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, repeaterA, true)
		newSender(t, ct, userA, userB, false, 7203).data(req)
		time.Sleep(100 * time.Millisecond)
		back = newSender(t, ct, userB, userA, false, 7204)
		back.repeater = other
		back.data(ans)
		synctest.Wait()
		release()

		got := byStream(waitDone(t, ct, database, 2))
		assert.Equal(t, models.CallResponseACK, got[7201].Response)
		assert.Equal(t, models.CallResponseACK, got[7203].Response, "the answer of the second check went to the first")
		assert.NotContains(t, got, uint(7202), "the first answer keeps a row")
		assert.NotContains(t, got, uint(7204), "the second answer keeps a row")
	})
}

// TestAnAnswerDoesNotWaitForAnUnrelatedWorker holds a worker that has no
// part in a radio check for longer than the response window while the
// check is answered. The answer must be set at once.
func TestAnAnswerDoesNotWaitForAnUnrelatedWorker(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		other := workerApart(repeaterA)
		req, ans := radioCheckBursts(t)

		newSender(t, ct, userA, userB, false, 7211).data(req)
		ct.Flush()
		release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, workerApart(repeaterA, other), true)
		back := newSender(t, ct, userB, userA, false, 7212)
		back.repeater = other
		back.data(ans)
		time.Sleep(calltracker.ResponseWindow + time.Second)

		got := byStream(rows(t, database))
		assert.Equal(t, models.CallResponseACK, got[7211].Response, "the answer waited for a worker it has no part in")
		assert.NotContains(t, got, uint(7212), "the answer keeps a row")
		release()
	})
}

// TestARowThatStartsAfterAnAnswerDoesNotTakeIt holds the worker of an ACK
// while a second confirmed message to the same radio starts after it and
// is shared in the KV. The ACK must go to the first message, which started
// before it, and not to the second through the KV.
func TestARowThatStartsAfterAnAnswerDoesNotTakeIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		first := confirmedText(t, "one")
		newSender(t, ct, userB, userA, false, 7301).data(first...)
		ct.Flush()
		time.Sleep(time.Second)

		other := workerApart(repeaterA)
		release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, other, true)
		ack := newSender(t, ct, userA, userB, false, 7302)
		ack.repeater = other
		ack.data(confirmedACK(t, first)...)
		time.Sleep(100 * time.Millisecond)
		newSender(t, ct, userB, userA, false, 7303).data(confirmedText(t, "two")...)
		synctest.Wait()
		release()

		got := byStream(waitDone(t, ct, database, 3))
		assert.Equal(t, models.CallResponseACK, got[7301].Response, "the first message has no ACK")
		assert.Equal(t, models.CallResponseNone, got[7303].Response, "a message that started after the ACK took it")
	})
}

// TestARowThatStartsAfterAnAnswerWithNoRowBeforeItDoesNotTakeIt holds
// the worker of an ACK that no message before it asked for, while a
// confirmed message to the same radio starts after it and is shared in the
// KV. The message must not get the ACK.
func TestARowThatStartsAfterAnAnswerWithNoRowBeforeItDoesNotTakeIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		other := workerApart(repeaterA)
		release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, other, true)
		ack := newSender(t, ct, userA, userB, false, 7311)
		ack.repeater = other
		ack.data(confirmedACK(t, confirmedText(t, "never sent"))...)
		time.Sleep(100 * time.Millisecond)
		newSender(t, ct, userB, userA, false, 7312).data(confirmedText(t, "after")...)
		synctest.Wait()
		release()

		got := byStream(waitDone(t, ct, database, 2))
		assert.Equal(t, models.CallResponseNone, got[7312].Response, "a message that started after the ACK took it")
	})
}

// TestAnAnswerMovesToAMessageTrackedAfterItThatStartedBefore tracks the
// bursts of a second message after its ACK was handled, with the times
// the hub got them, before the ACK, as a server whose read loop lags does.
// The ACK first goes to the first message and then moves to the second,
// and both rows and their updates on pubsub show it.
func TestAnAnswerMovesToAMessageTrackedAfterItThatStartedBefore(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, ps := newTracker(t)
		sub := ps.Subscribe("calls")
		t.Cleanup(func() { _ = sub.Close() })
		newSender(t, ct, userB, userA, false, 7401).data(confirmedText(t, "one")...)
		time.Sleep(4 * time.Second)
		ct.Flush()

		second := confirmedText(t, "two")
		sentAt := time.Now()
		time.Sleep(100 * time.Millisecond)
		ack := newSender(t, ct, userA, userB, false, 7403)
		ack.repeater = workerApart(repeaterA)
		ack.data(confirmedACK(t, second)...)
		ct.Flush()
		late := newSender(t, ct, userB, userA, false, 7402)
		late.at = sentAt
		late.data(second...)

		got := byStream(waitDone(t, ct, database, 3))
		assert.Equal(t, models.CallResponseNone, got[7401].Response, "the first message kept the ACK in the database")
		assert.Equal(t, models.CallResponseACK, got[7402].Response, "the second message has no ACK in the database")
		firstID, secondID := got[7401].ID, got[7402].ID

		var first []models.CallResponse
		last := map[uint]models.CallResponse{}
	drain:
		for {
			synctest.Wait()
			select {
			case msg := <-sub.Channel():
				var c models.Call
				require.NoError(t, json.Unmarshal(msg, &c))
				last[c.ID] = c.Response
				if c.ID == firstID {
					first = append(first, c.Response)
				}
			default:
				break drain
			}
		}
		assert.Contains(t, first, models.CallResponseACK, "the ACK on the first message was not published")
		assert.Equal(t, models.CallResponseNone, last[firstID], "the last update of the first message keeps the ACK")
		assert.Equal(t, models.CallResponseACK, last[secondID], "the last update of the second message has no ACK")
	})
}

// TestARowOfAnotherReplicaThatStartsAfterAnAnswerDoesNotTakeIt is
// TestARowThatStartsAfterAnAnswerDoesNotTakeIt across two replicas: the
// messages are on one, and the ACK is on the other, on a worker that lags.
func TestARowOfAnotherReplicaThatStartsAfterAnAnswerDoesNotTakeIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		one, two, database := newReplicas(t)
		first := confirmedText(t, "one")
		newSender(t, two, userB, userA, false, 7501).data(first...)
		two.Flush()
		time.Sleep(time.Second)

		other := workerApart(repeaterA)
		release := calltracker.HoldWorker(one, models.RepeaterTypeMMDVM, other, true)
		ack := newSender(t, one, userA, userB, false, 7502)
		ack.repeater = other
		ack.data(confirmedACK(t, first)...)
		time.Sleep(100 * time.Millisecond)
		newSender(t, two, userB, userA, false, 7503).data(confirmedText(t, "two")...)
		two.Flush()
		release()

		got := map[uint]models.Call{}
		bubble.Eventually(t, func() bool {
			one.Flush()
			two.Flush()
			clear(got)
			for _, c := range rows(t, database) {
				if c.Active {
					return false
				}
				got[c.StreamID] = c
			}
			return len(got) == 3 && got[7501].Response != models.CallResponseNone
		}, 8*time.Second, 50*time.Millisecond, "the ACK is set on the first message")
		assert.Equal(t, models.CallResponseACK, got[7501].Response)
		assert.Equal(t, models.CallResponseNone, got[7503].Response, "a message of another replica that started after the ACK took it")
	})
}

// TestTwoReplicasThatTakeOneAnswerOverAgree tracks an ACK on one replica,
// where it goes to the only message there is, and then two messages that
// started before it, one on each replica, at once, in both orders. The
// ACK must end on the message that started last before it, and the other
// two must have none.
func TestTwoReplicasThatTakeOneAnswerOverAgree(t *testing.T) {
	t.Parallel()
	for _, first := range []string{"one", "two"} {
		t.Run(first+" first", func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				one, two, database := newReplicas(t)
				newSender(t, one, userB, userA, false, 7701).data(confirmedText(t, "first")...)
				one.Flush()
				time.Sleep(time.Second)
				early := time.Now()
				time.Sleep(100 * time.Millisecond)
				late := time.Now()
				time.Sleep(100 * time.Millisecond)
				msg := confirmedText(t, "late")
				ack := newSender(t, one, userA, userB, false, 7702)
				ack.repeater = workerApart(repeaterA)
				ack.data(confirmedACK(t, msg)...)
				one.Flush()

				onOne := newSender(t, one, userB, userA, false, 7703)
				onOne.at = early
				onTwo := newSender(t, two, userB, userA, false, 7704)
				onTwo.at = late
				senders := []*sender{onOne, onTwo}
				if first == "two" {
					senders[0], senders[1] = senders[1], senders[0]
				}
				done := make(chan struct{})
				for _, s := range senders {
					go func() {
						s.data(msg...)
						done <- struct{}{}
					}()
				}
				<-done
				<-done

				got := map[uint]models.Call{}
				bubble.Eventually(t, func() bool {
					one.Flush()
					two.Flush()
					clear(got)
					for _, c := range rows(t, database) {
						if c.Active {
							return false
						}
						got[c.StreamID] = c
					}
					return len(got) == 4 && got[7704].Response == models.CallResponseACK && got[7701].Response == models.CallResponseNone && got[7703].Response == models.CallResponseNone
				}, 8*time.Second, 50*time.Millisecond, "the ACK did not end on the message that started last before it")
			})
		})
	}
}

// TestARowOfAnotherReplicaThatStartsAfterAnAnswerDoesNotTakeItOnRedis is
// TestARowOfAnotherReplicaThatStartsAfterAnAnswerDoesNotTakeIt on Postgres
// and Redis, on the wall clock. The times the hub got the bursts are given,
// so the test does not depend on how loaded the machine is.
func TestARowOfAnotherReplicaThatStartsAfterAnAnswerDoesNotTakeItOnRedis(t *testing.T) {
	t.Parallel()
	database, ps, store := backendDeps(t, testutils.PostgresRedisBackend())
	one := calltracker.NewCallTracker(database, ps, store)
	t.Cleanup(one.Stop)
	two := calltracker.NewCallTracker(database, ps, store)
	t.Cleanup(two.Stop)
	sub := ps.Subscribe("calls")
	t.Cleanup(func() { _ = sub.Close() })

	base := time.Now().Add(-5 * time.Second)
	first := confirmedText(t, "one")
	m1 := newSender(t, two, userB, userA, false, 7601)
	m1.at = base
	m1.data(first...)
	two.Flush()

	other := workerApart(repeaterA)
	release := calltracker.HoldWorker(one, models.RepeaterTypeMMDVM, other, true)
	ack := newSender(t, one, userA, userB, false, 7602)
	ack.repeater = other
	ack.at = base.Add(time.Second)
	ack.data(confirmedACK(t, first)...)
	m2 := newSender(t, two, userB, userA, false, 7603)
	m2.at = base.Add(time.Second + 100*time.Millisecond)
	m2.data(confirmedText(t, "two")...)
	two.Flush()
	release()
	one.Flush()
	ids := byStream(rows(t, database))
	firstID, secondID := ids[7601].ID, ids[7603].ID
	require.NotZero(t, firstID)
	require.NotZero(t, secondID)

	ctx, cancel := context.WithTimeout(t.Context(), 2*calltracker.ResponseWindow)
	defer cancel()
	last := map[uint]models.Call{}
	ended := func() bool {
		return last[firstID].Response == models.CallResponseACK && !last[firstID].Active && last[secondID].ID != 0 && !last[secondID].Active
	}
	for !ended() {
		select {
		case msg := <-sub.Channel():
			var c models.Call
			require.NoError(t, json.Unmarshal(msg, &c))
			last[c.ID] = c
		case <-ctx.Done():
			t.Fatalf("the rows did not end with the ACK on the first message: %+v", last)
		}
	}
	got := byStream(rows(t, database))
	assert.Equal(t, models.CallResponseACK, got[7601].Response)
	assert.Equal(t, models.CallResponseNone, got[7603].Response, "a message of another replica that started after the ACK took it")
}

func backendDeps(t *testing.T, be testutils.Backend) (*gorm.DB, pubsub.PubSub, kv.KV) {
	t.Helper()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	be.Setup(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.TODO(), &cfg)
	require.NoError(t, err)
	store, err := kv.MakeKV(context.TODO(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() {
		sqlDB, _ := database.DB()
		_ = sqlDB.Close()
		_ = ps.Close()
		_ = store.Close()
	})
	require.NoError(t, database.Create(&models.User{ID: userA, Callsign: "N6CALL", Username: "a", Approved: true}).Error)
	require.NoError(t, database.Create(&models.User{ID: userB, Callsign: "N8CALL", Username: "b", Approved: true}).Error)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: repeaterA, Callsign: "RPT1"},
		OwnerID:               userA,
		Type:                  models.RepeaterTypeMMDVM,
	}).Error)
	return database, ps, store
}
