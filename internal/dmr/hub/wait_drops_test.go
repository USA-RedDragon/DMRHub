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
	"errors"
	"fmt"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func dropped(reason string) float64 {
	return testutil.ToFloat64(metrics.NewMetrics().IngressDroppedBurstsTotal.WithLabelValues(reason))
}

type heldLastSeen struct {
	kv.KV
	open chan struct{}
}

func (k *heldLastSeen) Get(ctx context.Context, key string) ([]byte, error) {
	if strings.HasPrefix(key, "dmrhub:lastheard:") {
		<-k.open
	}
	return k.KV.Get(ctx, key) //nolint:wrapcheck
}

func TestABurstThatTheOwnerLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{Transmit: config.TransmitMembers}}})
		held := holdQueries(t, database, "users")
		lookupWait, transmit := dropped(metrics.DropReasonLookupWait), dropped(metrics.DropReasonTransmit)

		held.armed.Store(true)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 63001, true, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()

		assert.InDelta(t, 1, dropped(metrics.DropReasonLookupWait)-lookupWait, 0, "the burst that the owner lookup gave up on")
		assert.InDelta(t, 0, dropped(metrics.DropReasonTransmit)-transmit, 0, "no burst refused by the transmit setting")
		held.release()
	})
}

func TestABurstThatASlotLockRefusedForASlowLookupIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedTalkgroup(t, database, 1, "TG1")
		require.NoError(t, database.Create(&models.UserRadioID{UserID: testSrcID, RadioID: 1000101}).Error)
		require.NoError(t, models.ReplaceRoutingRules(database, testRepeaterID, models.SlotRuleKinds(), []models.RoutingRule{{Kind: models.RoutingRuleSlotUser, Slot: 1, SubjectID: testSrcID}}))
		h.RulesChanged(testRepeaterID)
		knowTalkgroups(t, h)
		held := holdQueries(t, database, "users")
		lookupWait := dropped(metrics.DropReasonLookupWait)

		held.armed.Store(true)
		p := makeVoicePacket(1, 63002, true, false)
		p.Src = 1000101
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()

		assert.InDelta(t, 1, dropped(metrics.DropReasonLookupWait)-lookupWait, 0, "the burst that the slot lock refused when the owner lookup gave up")
		held.release()
	})
}

func TestAPrivateBurstThatTheLastSeenReadGaveUpOnIsCountedAsALastSeenWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		held := &heldLastSeen{open: make(chan struct{})}
		h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
			held.KV = k
			return held
		})
		t.Cleanup(func() { close(held.open) })
		seedUser(t, database, testSrcID, "SENDER")
		seedUser(t, database, 1000002, "TARGET")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		loadLookups(t, h, nil, []uint{testSrcID, 1000002})
		lastSeenWait := dropped(metrics.DropReasonLastSeenWait)

		p := makeVoicePacket(1000002, 63003, false, false)
		h.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()

		assert.InDelta(t, 1, dropped(metrics.DropReasonLastSeenWait)-lastSeenWait, 0, "the private burst that the read of where its target was heard gave up on")
	})
}

func TestABurstThatAStreamClaimCheckGaveUpOnIsCountedAsAClaimWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		slow := &slowKV{hang: make(chan struct{})}
		h, _, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
			slow.KV = k
			return slow
		})
		t.Cleanup(func() {
			slow.on.Store(false)
			close(slow.hang)
		})
		h.SetStreamClaimTiming(time.Minute, time.Second)
		burst := func(repeater uint) models.Packet {
			p := makeVoicePacket(1, 42, true, false)
			p.Repeater = repeater
			return p
		}
		require.True(t, h.AdmitStream(t.Context(), burst(100001), models.RepeaterTypeMMDVM))
		claimWait, other := dropped(metrics.DropReasonClaimWait), dropped(metrics.DropReasonStreamFromOtherSource)

		time.Sleep(2 * time.Second)
		slow.on.Store(true)
		assert.False(t, h.AdmitStream(t.Context(), burst(100002), models.RepeaterTypeMMDVM), "a copy is dropped while the claim of its stream holds")

		assert.InDelta(t, 1, dropped(metrics.DropReasonClaimWait)-claimWait, 0, "the copy whose claim check gave up")
		assert.InDelta(t, 0, dropped(metrics.DropReasonStreamFromOtherSource)-other, 0, "no copy dropped on an answer of the KV store")
	})
}

type timingOutKV struct {
	kv.KV
	on     atomic.Bool
	prefix string
}

func (k *timingOutKV) fail(op, key string) error {
	if k.on.Load() && strings.HasPrefix(key, k.prefix) {
		return fmt.Errorf("redis %s %s: %w", op, key, os.ErrDeadlineExceeded)
	}
	return nil
}

func (k *timingOutKV) Get(ctx context.Context, key string) ([]byte, error) {
	if err := k.fail("get", key); err != nil {
		return nil, err
	}
	return k.KV.Get(ctx, key) //nolint:wrapcheck
}

func (k *timingOutKV) SetNX(ctx context.Context, key string, value string, ttl time.Duration) (bool, error) {
	if err := k.fail("setnx", key); err != nil {
		return false, err
	}
	return k.KV.SetNX(ctx, key, value, ttl) //nolint:wrapcheck
}

func (k *timingOutKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if err := k.fail("expire", key); err != nil {
		return err
	}
	return k.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

func TestABurstThatAStreamClaimCheckTimedOutOnIsCountedAsAClaimWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		timing := &timingOutKV{prefix: "dmrhub:stream_claim:"}
		h, _, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
			timing.KV = k
			return timing
		})
		h.SetStreamClaimTiming(time.Minute, time.Second)
		burst := func(repeater uint) models.Packet {
			p := makeVoicePacket(1, 43, true, false)
			p.Repeater = repeater
			return p
		}
		require.True(t, h.AdmitStream(t.Context(), burst(100001), models.RepeaterTypeMMDVM))
		claimWait, other := dropped(metrics.DropReasonClaimWait), dropped(metrics.DropReasonStreamFromOtherSource)

		time.Sleep(2 * time.Second)
		timing.on.Store(true)
		assert.False(t, h.AdmitStream(t.Context(), burst(100002), models.RepeaterTypeMMDVM), "a copy is dropped while the claim of its stream holds")

		assert.InDelta(t, 1, dropped(metrics.DropReasonClaimWait)-claimWait, 0, "the copy whose claim check timed out")
		assert.InDelta(t, 0, dropped(metrics.DropReasonStreamFromOtherSource)-other, 0, "no copy dropped on an answer of the KV store")
	})
}

func TestAPrivateBurstThatTheLastSeenReadTimedOutOnIsCountedAsALastSeenWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		timing := &timingOutKV{prefix: "dmrhub:lastheard:"}
		h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
			timing.KV = k
			return timing
		})
		seedUser(t, database, testSrcID, "SENDER")
		seedUser(t, database, 1000002, "TARGET")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		loadLookups(t, h, nil, []uint{testSrcID, 1000002})
		lastSeenWait := dropped(metrics.DropReasonLastSeenWait)

		timing.on.Store(true)
		h.RoutePacket(context.Background(), makeVoicePacket(1000002, 63004, false, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()

		assert.InDelta(t, 1, dropped(metrics.DropReasonLastSeenWait)-lastSeenWait, 0, "the private burst that the read of where its target was heard timed out on")
	})
}

func TestABurstThatTheOwnerLookupTimedOutOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{Transmit: config.TransmitMembers}}})
		var on atomic.Bool
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:time_out_users", func(tx *gorm.DB) {
			if on.Load() && tx.Statement.Table == "users" {
				_ = tx.AddError(context.DeadlineExceeded)
			}
		}))
		lookupWait, transmit := dropped(metrics.DropReasonLookupWait), dropped(metrics.DropReasonTransmit)

		on.Store(true)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 63005, true, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()

		assert.InDelta(t, 1, dropped(metrics.DropReasonLookupWait)-lookupWait, 0, "the burst that the owner lookup timed out on")
		assert.InDelta(t, 0, dropped(metrics.DropReasonTransmit)-transmit, 0, "no burst refused by the transmit setting")
	})
}

func TestAGroupBurstToATalkgroupThatTheLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		held := holdQueries(t, database, "talkgroups")
		seedTalkgroup(t, database, 77, "TG77")
		held.armed.Store(true)
		before := dropped(metrics.DropReasonLookupWait)
		h.RoutePacket(context.Background(), makeVoicePacket(77, 63006, true, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()
		got := dropped(metrics.DropReasonLookupWait) - before
		assert.InDelta(t, 1, got, 0, "the group burst that the talkgroup lookup gave up on")
		held.release()
	})
}

func TestAPrivateBurstToARepeaterThatTheLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedRepeater(t, database, 100009, testSrcID)
		require.NoError(t, h.KnowRepeater(testRepeaterID))
		held := holdQueries(t, database, "repeaters")
		held.armed.Store(true)
		before := dropped(metrics.DropReasonLookupWait)
		h.RoutePacket(context.Background(), makeVoicePacket(100009, 63007, false, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()
		got := dropped(metrics.DropReasonLookupWait) - before
		assert.InDelta(t, 1, got, 0, "the private burst to a repeater that the repeater lookup gave up on")
		held.release()
	})
}

func TestAPrivateBurstToARadioHeardOnARepeaterThatTheLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedUser(t, database, 1000002, "TARGET")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedRepeater(t, database, 100009, 1000002)
		require.NoError(t, h.KnowRepeater(testRepeaterID))
		loadLookups(t, h, nil, []uint{testSrcID, 1000002})
		require.NoError(t, h.StoreSighting(1000002, 100009, time.Now(), true))
		held := holdQueries(t, database, "repeaters")
		held.armed.Store(true)
		lastSeenWait := dropped(metrics.DropReasonLastSeenWait)
		before := dropped(metrics.DropReasonLookupWait)
		h.RoutePacket(context.Background(), makeVoicePacket(1000002, 63008, false, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()
		got := dropped(metrics.DropReasonLookupWait) - before
		assert.InDelta(t, 1, got, 0, "the private burst whose target's repeater lookup gave up")
		assert.InDelta(t, 0, dropped(metrics.DropReasonLastSeenWait)-lastSeenWait, 0, "the last-seen read answered")
		held.release()
	})
}

func TestABurstToThePeersThatThePeerLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedTalkgroup(t, database, 1, "TG1")
		knowTalkgroups(t, h)
		h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
		require.NoError(t, database.Create(&models.Peer{ID: 3100100, OwnerID: testSrcID, Egress: true}).Error)
		var failing atomic.Bool
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:fail_peers", func(tx *gorm.DB) {
			if failing.Load() && tx.Statement.Table == "peers" {
				_ = tx.AddError(errors.New("the database is down"))
			}
		}))
		held := holdQueries(t, database, "peers")
		failing.Store(true)
		h.PeerChanged(3100100)
		failing.Store(false)
		held.armed.Store(true)
		before := dropped(metrics.DropReasonLookupWait)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 63009, true, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()
		got := dropped(metrics.DropReasonLookupWait) - before
		assert.InDelta(t, 1, got, 0, "the burst that the peer lookup gave up on")
		held.release()
	})
}

func TestAGroupBurstToARepeaterThatTheLookupGaveUpOnIsCountedAsALookupWait(t *testing.T) { //nolint:paralleltest
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, testSrcID, "SENDER")
		seedRepeater(t, database, testRepeaterID, testSrcID)
		seedRepeater(t, database, 100002, testSrcID)
		seedTalkgroup(t, database, 1, "TG1")
		var rpt models.Repeater
		require.NoError(t, database.First(&rpt, 100002).Error)
		require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
		srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
		t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
		h.ActivateRepeater(context.Background(), testRepeaterID)
		h.ActivateRepeater(context.Background(), 100002)
		knowTalkgroups(t, h)
		waitAllReceive(t, h, srv, 1, 1, 64000)

		var failing atomic.Bool
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:fail_repeaters", func(tx *gorm.DB) {
			if failing.Load() && tx.Statement.Table == "repeaters" {
				_ = tx.AddError(errors.New("the database is down"))
			}
		}))
		held := holdQueries(t, database, "repeaters")
		failing.Store(true)
		h.CodecChanged(100002)
		failing.Store(false)
		held.armed.Store(true)
		before := dropped(metrics.DropReasonLookupWait)
		h.RoutePacket(context.Background(), makeVoicePacket(1, 64001, true, false), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		synctest.Wait()
		got := dropped(metrics.DropReasonLookupWait) - before
		assert.InDelta(t, 1, got, 0, "the group burst that the lookup of a repeater that takes it gave up on")
		held.release()
	})
}
