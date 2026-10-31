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
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
