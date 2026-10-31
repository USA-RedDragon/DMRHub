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
	"fmt"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/assert"
)

// gateKV holds the first KV call of an armed race until the call of the
// other side of the race is done, so that the two run in the order that
// went wrong. It knows the calls of the match in one Update and the
// calls of the lists that an earlier design used.
type gateKV struct {
	kv.KV
	mu      sync.Mutex
	mode    string
	state   int
	first   chan struct{}
	release chan struct{}
}

func newGate(inner kv.KV) *gateKV {
	return &gateKV{KV: inner, first: make(chan struct{}), release: make(chan struct{})}
}

func (g *gateKV) arm(mode string) {
	g.mu.Lock()
	g.mode, g.state = mode, 1
	g.mu.Unlock()
}

func (g *gateKV) Update(ctx context.Context, key string, ttl time.Duration, fn func(old []byte) ([]byte, error)) error {
	g.mu.Lock()
	if g.mode != "" && strings.Contains(key, ":match:") {
		switch g.state {
		case 1:
			g.state = 2
			g.mu.Unlock()
			close(g.first)
			<-g.release
			return gated(g.KV.Update(ctx, key, ttl, fn))
		case 2:
			g.state = 3
			g.mu.Unlock()
			err := g.KV.Update(ctx, key, ttl, fn)
			close(g.release)
			return gated(err)
		}
	}
	g.mu.Unlock()
	return gated(g.KV.Update(ctx, key, ttl, fn))
}

func (g *gateKV) LRange(ctx context.Context, key string) ([][]byte, error) {
	res, err := g.KV.LRange(ctx, key)
	g.mu.Lock()
	mode, state := g.mode, g.state
	switch {
	case mode == "dekker" && state == 1 && strings.Contains(key, ":rows:"):
		g.state = 2
		g.mu.Unlock()
		close(g.first)
		<-g.release
		return res, gated(err)
	case mode == "dekker" && state == 2 && strings.Contains(key, ":answers:"):
		g.state = 3
		g.mu.Unlock()
		close(g.release)
		return res, gated(err)
	case mode == "claims" && state == 1 && strings.Contains(key, ":answers:"):
		g.state = 2
		g.mu.Unlock()
		close(g.first)
		<-g.release
		return res, gated(err)
	case mode == "claims" && state == 3 && strings.Contains(key, ":answers:"):
		g.state = 4
		g.mu.Unlock()
		close(g.release)
		return res, gated(err)
	}
	g.mu.Unlock()
	return res, gated(err)
}

func (g *gateKV) RPushTrim(ctx context.Context, key string, value []byte, keep int64, ttl time.Duration) error {
	err := g.KV.RPushTrim(ctx, key, value, keep, ttl)
	g.mu.Lock()
	if g.mode == "claims" && g.state == 2 && strings.Contains(key, ":answers:") {
		g.state = 3
	}
	g.mu.Unlock()
	return gated(err)
}

func gated(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("gated: %w", err)
}

// TestALateTakeoverReachesARowRememberedLongBefore moves an ACK to a
// message that started before it more than the response window after the
// first message was made. The first message must lose the ACK. (Second
// review, R1.)
func TestALateTakeoverReachesARowRememberedLongBefore(t *testing.T) {
	t.Parallel()
	for _, gap := range []time.Duration{0, 500 * time.Millisecond} {
		t.Run(gap.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ct, database, _ := newTracker(t)
				newSender(t, ct, userB, userA, false, 9101).data(confirmedText(t, "one")...)
				ct.Flush()
				time.Sleep(9500 * time.Millisecond)
				second := confirmedText(t, "two")
				sentAt := time.Now()
				time.Sleep(100 * time.Millisecond)
				ack := newSender(t, ct, userA, userB, false, 9103)
				ack.repeater = workerApart(repeaterA)
				ack.data(confirmedACK(t, second)...)
				ct.Flush()
				time.Sleep(gap)
				late := newSender(t, ct, userB, userA, false, 9102)
				late.at = sentAt
				late.data(second...)
				got := byStream(waitDone(t, ct, database, 3))
				assert.Equal(t, models.CallResponseNone, got[9101].Response, "the first message kept the ACK")
				assert.Equal(t, models.CallResponseACK, got[9102].Response, "the second message has no ACK")
			})
		})
	}
}

// TestAnAnswerAndALateRowThatRaceStillMeet runs the answer of a message
// and the row of the message so that each reads before the other writes,
// as far as the design lets them. The ACK must end on the message. (Second
// review, R2.)
func TestAnAnswerAndALateRowThatRaceStillMeet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, store := trackerDeps(t)
		g := newGate(store)
		ct := calltracker.NewCallTracker(database, ps, g)
		t.Cleanup(ct.Stop)
		newSender(t, ct, userB, userA, false, 9201).data(confirmedText(t, "one")...)
		ct.Flush()
		time.Sleep(time.Second)
		second := confirmedText(t, "two")
		sentAt := time.Now()
		time.Sleep(100 * time.Millisecond)
		g.arm("dekker")
		ack := newSender(t, ct, userA, userB, false, 9203)
		ack.repeater = workerApart(repeaterA)
		ack.data(confirmedACK(t, second)...)
		<-g.first
		late := newSender(t, ct, userB, userA, false, 9202)
		late.at = sentAt
		late.data(second...)
		got := byStream(waitDone(t, ct, database, 3))
		assert.Equal(t, models.CallResponseNone, got[9201].Response, "the first message has the ACK of the second")
		assert.Equal(t, models.CallResponseACK, got[9202].Response, "the second message lost its ACK")
	})
}

// TestTwoLateRowsThatRaceForAnAnswerLeaveItOnOne makes two messages that
// started before an ACK after it, so that the second reads before the
// first writes, as far as the design lets them. Only the message that
// started last before the ACK may keep it. (Second review, R3.)
func TestTwoLateRowsThatRaceForAnAnswerLeaveItOnOne(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, store := trackerDeps(t)
		g := newGate(store)
		ct := calltracker.NewCallTracker(database, ps, g)
		t.Cleanup(ct.Stop)
		other := workerApart(repeaterA)
		third := workerApart(repeaterA, other)
		newSender(t, ct, userB, userA, false, 9301).data(confirmedText(t, "first")...)
		ct.Flush()
		time.Sleep(time.Second)
		early := time.Now()
		time.Sleep(100 * time.Millisecond)
		lateAt := time.Now()
		time.Sleep(100 * time.Millisecond)
		msg := confirmedText(t, "late")
		ack := newSender(t, ct, userA, userB, false, 9302)
		ack.repeater = other
		ack.data(confirmedACK(t, msg)...)
		ct.Flush()
		synctest.Wait()

		g.arm("claims")
		m3 := newSender(t, ct, userB, userA, false, 9304)
		m3.repeater, m3.at = third, lateAt
		m3.data(msg...)
		<-g.first
		m2 := newSender(t, ct, userB, userA, false, 9303)
		m2.at = early
		m2.data(msg...)
		got := byStream(waitDone(t, ct, database, 4))
		assert.Equal(t, models.CallResponseNone, got[9301].Response)
		assert.Equal(t, models.CallResponseNone, got[9303].Response, "the message that started earlier kept the ACK")
		assert.Equal(t, models.CallResponseACK, got[9304].Response)
	})
}

// TestAnAnswerOutlivesTheWindowOfItsMatch holds the worker of an answered
// radio check while the check is answered again and the response window,
// or the whole life of the match, passes. The check must keep its ACK.
// (Second review, R4.)
func TestAnAnswerOutlivesTheWindowOfItsMatch(t *testing.T) {
	t.Parallel()
	for _, wait := range []time.Duration{calltracker.ResponseWindow + time.Second, 2 * time.Minute} {
		t.Run(wait.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ct, database, _ := newTracker(t)
				other := workerApart(repeaterA)
				req, ans := radioCheckBursts(t)
				newSender(t, ct, userA, userB, false, 9401).data(req)
				ct.Flush()
				back := newSender(t, ct, userB, userA, false, 9402)
				back.repeater = other
				back.data(ans)
				ct.Flush()
				time.Sleep(2 * time.Second)
				ct.Flush()
				release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, repeaterA, true)
				back.data(ans)
				synctest.Wait()
				time.Sleep(wait)
				release()
				got := byStream(waitDone(t, ct, database, 1))
				assert.Equal(t, models.CallResponseACK, got[9401].Response, "the ACK was wiped")
			})
		})
	}
}

// readThenHold holds the first read of a match after arm until release.
type readThenHold struct {
	kv.KV
	mu      sync.Mutex
	armed   bool
	first   chan struct{}
	release chan struct{}
}

func newReadThenHold(inner kv.KV) *readThenHold {
	return &readThenHold{KV: inner, first: make(chan struct{}), release: make(chan struct{})}
}

func (g *readThenHold) arm() {
	g.mu.Lock()
	g.armed = true
	g.mu.Unlock()
}

func (g *readThenHold) Get(ctx context.Context, key string) ([]byte, error) {
	b, err := g.KV.Get(ctx, key)
	g.mu.Lock()
	if g.armed && strings.Contains(key, ":match:") {
		g.armed = false
		g.mu.Unlock()
		close(g.first)
		<-g.release
		return b, gated(err)
	}
	g.mu.Unlock()
	return b, gated(err)
}

// TestAFinishThatRacesASettleKeepsTheSettle holds the read of the match
// that the finish of a message makes, while a late message takes its ACK
// over. The first message must end with no response. (Third review, F1.)
func TestAFinishThatRacesASettleKeepsTheSettle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, store := trackerDeps(t)
		g := newReadThenHold(store)
		ct := calltracker.NewCallTracker(database, ps, g)
		t.Cleanup(ct.Stop)
		sub := ps.Subscribe("calls")
		var pubMu sync.Mutex
		pubAll := map[uint][]models.CallResponse{}
		subCtx, subStop := context.WithCancel(context.Background())
		go func() {
			for {
				select {
				case msg := <-sub.Channel():
					var c models.Call
					if json.Unmarshal(msg, &c) == nil {
						pubMu.Lock()
						pubAll[c.ID] = append(pubAll[c.ID], c.Response)
						pubMu.Unlock()
					}
				case <-subCtx.Done():
					return
				}
			}
		}()
		t.Cleanup(func() { subStop(); _ = sub.Close() })
		other := workerApart(repeaterA)
		third := workerApart(repeaterA, other)
		newSender(t, ct, userB, userA, false, 9501).data(confirmedText(t, "one")...)
		ct.Flush()
		time.Sleep(500 * time.Millisecond)
		second := confirmedText(t, "two")
		sentAt := time.Now()
		time.Sleep(100 * time.Millisecond)
		ack := newSender(t, ct, userA, userB, false, 9503)
		ack.repeater = other
		ack.data(confirmedACK(t, second)...)
		ct.Flush()
		synctest.Wait()
		g.arm()
		<-g.first
		late := newSender(t, ct, userB, userA, false, 9502)
		late.repeater, late.at = third, sentAt
		late.data(second...)
		synctest.Wait()
		close(g.release)
		got := byStream(waitDone(t, ct, database, 3))
		time.Sleep(time.Second)
		synctest.Wait()
		pubMu.Lock()
		pub := map[uint][]models.CallResponse{}
		for k, v := range pubAll {
			pub[k] = v
		}
		pubMu.Unlock()
		assert.Equal(t, models.CallResponseNone, pub[got[9501].ID][len(pub[got[9501].ID])-1], "the last update of the first message keeps the ACK")
		assert.Equal(t, models.CallResponseNone, got[9501].Response, "M1 kept the ACK in the DB")
		assert.Equal(t, models.CallResponseACK, got[9502].Response)
	})
}

// TestAFinishThatRacesASettleKeepsTheSettleAcrossReplicas is
// TestAFinishThatRacesASettleKeepsTheSettle with the first message on one
// replica and the ACK and the late message on another. (Third review, F1.)
func TestAFinishThatRacesASettleKeepsTheSettleAcrossReplicas(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		database, ps, store := trackerDeps(t)
		g := newReadThenHold(store)
		one := calltracker.NewCallTracker(database, ps, g)
		t.Cleanup(one.Stop)
		two := calltracker.NewCallTracker(database, ps, g)
		t.Cleanup(two.Stop)
		other := workerApart(repeaterA)
		third := workerApart(repeaterA, other)
		newSender(t, two, userB, userA, false, 9511).data(confirmedText(t, "one")...)
		two.Flush()
		time.Sleep(500 * time.Millisecond)
		second := confirmedText(t, "two")
		sentAt := time.Now()
		time.Sleep(100 * time.Millisecond)
		ack := newSender(t, one, userA, userB, false, 9513)
		ack.repeater = other
		ack.data(confirmedACK(t, second)...)
		one.Flush()
		synctest.Wait()
		two.Flush()
		synctest.Wait()
		g.arm()
		<-g.first
		late := newSender(t, one, userB, userA, false, 9512)
		late.repeater, late.at = third, sentAt
		late.data(second...)
		synctest.Wait()
		close(g.release)
		var got map[uint]models.Call
		bubble.Eventually(t, func() bool {
			one.Flush()
			two.Flush()
			got = byStream(rows(t, database))
			for _, c := range got {
				if c.Active {
					return false
				}
			}
			return len(got) == 3
		}, 8*time.Second, 50*time.Millisecond)
		time.Sleep(time.Second)
		one.Flush()
		two.Flush()
		got = byStream(rows(t, database))
		assert.Equal(t, models.CallResponseNone, got[9511].Response, "M1 kept the ACK in the DB")
		assert.Equal(t, models.CallResponseACK, got[9512].Response)
	})
}

// TestARepeatedAnswerBurstThatFindsARequestDropsItsOwnRow answers a radio
// check before the check is heard, so the answer makes a row of its own,
// and repeats the answer after the check started. The check takes the
// answer, and the row of the answer must go. (Third review, F2.)
func TestARepeatedAnswerBurstThatFindsARequestDropsItsOwnRow(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ct, database, _ := newTracker(t)
		other := workerApart(repeaterA)
		req, ans := radioCheckBursts(t)
		back := newSender(t, ct, userB, userA, false, 9602)
		back.repeater = other
		back.data(ans)
		ct.Flush()
		time.Sleep(10 * time.Millisecond)
		newSender(t, ct, userA, userB, false, 9601).data(req)
		ct.Flush()
		time.Sleep(10 * time.Millisecond)
		back.data(ans)
		ct.Flush()

		got := byStream(waitDone(t, ct, database, 1))
		assert.Equal(t, models.CallResponseACK, got[9601].Response, "the check has no ACK")
		assert.NotContains(t, got, uint(9602), "the answer kept a row of its own")
	})
}

// TestAPrunedAnswerLeavesItsRowAsItIs holds the worker of an answered
// radio check while the pair keeps sending checks, until the answer is
// older than the match keeps. The held check must keep its ACK. (Third
// review, F3.)
func TestAPrunedAnswerLeavesItsRowAsItIs(t *testing.T) {
	t.Parallel()
	for _, total := range []time.Duration{65 * time.Second, 75 * time.Second} {
		t.Run(total.String(), func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				ct, database, _ := newTracker(t)
				other := workerApart(repeaterA)
				third := workerApart(repeaterA, other)
				req, ans := radioCheckBursts(t)
				newSender(t, ct, userA, userB, false, 9801).data(req)
				ct.Flush()
				release := calltracker.HoldWorker(ct, models.RepeaterTypeMMDVM, repeaterA, true)
				time.Sleep(100 * time.Millisecond)
				back := newSender(t, ct, userB, userA, false, 9802)
				back.repeater = other
				back.data(ans)
				synctest.Wait()
				time.Sleep(40 * time.Second)
				r3 := newSender(t, ct, userA, userB, false, 9803)
				r3.repeater = third
				r3.data(req)
				synctest.Wait()
				time.Sleep(total - 40*time.Second)
				r4 := newSender(t, ct, userA, userB, false, 9804)
				r4.repeater = third
				r4.data(req)
				synctest.Wait()
				release()
				got := byStream(waitDone(t, ct, database, 3))
				assert.Equal(t, models.CallResponseACK, got[9801].Response, "the ACK was wiped")
			})
		})
	}
}

// TestARequestHeardAfterItsAnswerTakesIt sends the answer of a radio check
// before the check, by a gap within the response window, on one replica
// and on two. The check must take the answer and the answer must keep no
// row of its own. (Third review, F4.)
func TestARequestHeardAfterItsAnswerTakesIt(t *testing.T) {
	t.Parallel()
	for _, replicas := range []int{1, 2} {
		for _, gap := range []time.Duration{time.Millisecond, 50 * time.Millisecond, 500 * time.Millisecond, 9 * time.Second} {
			t.Run(fmt.Sprintf("%d replicas %v", replicas, gap), func(t *testing.T) {
				t.Parallel()
				synctest.Test(t, func(t *testing.T) {
					database, ps, store := trackerDeps(t)
					one := calltracker.NewCallTracker(database, ps, store)
					t.Cleanup(one.Stop)
					two := one
					if replicas == 2 {
						two = calltracker.NewCallTracker(database, ps, store)
						t.Cleanup(two.Stop)
					}
					req, ans := radioCheckBursts(t)
					back := newSender(t, two, userB, userA, false, 9702)
					back.repeater = workerApart(repeaterA)
					back.data(ans)
					two.Flush()
					time.Sleep(gap)
					newSender(t, one, userA, userB, false, 9701).data(req)
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
						_, own := got[9702]
						return !own && got[9701].Response == models.CallResponseACK
					}, 8*time.Second, 50*time.Millisecond, "the check did not take its answer: %v", got)
				})
			})
		}
	}
}
