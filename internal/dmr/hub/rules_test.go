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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func floodConfig() config.Config {
	return config.Config{DMR: config.DMR{PrivateCalls: config.PrivateCalls{Voice: config.PrivateRoute{Unknown: config.PrivateUnknownFlood}}}}
}

const (
	ruleSender   = uint(100001)
	ruleReceiver = uint(100002)
	listedUser   = uint(1000001)
	otherUser    = uint(1000002)
	listedRadio  = uint(1000101)
	unknownRadio = uint(1000009)
)

type rulesEnv struct {
	hub  *hub.Hub
	db   *gorm.DB
	ps   pubsub.PubSub
	kv   kv.KV
	srv  *hub.ServerHandle
	next uint
}

func newRulesHub(t *testing.T, database *gorm.DB, ps pubsub.PubSub, kvStore kv.KV) *hub.Hub {
	t.Helper()
	ct := calltracker.NewCallTracker(database.WithContext(context.WithValue(context.Background(), trackerQuery{}, true)), ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	h.LiftSlotRate(true)
	h.SetConfig(floodConfig())
	t.Cleanup(func() { h.Stop(context.Background()) })
	return h
}

func rulesSetup(t *testing.T, be testutils.Backend) *rulesEnv {
	t.Helper()
	c, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(&c)
	be.Migrated().Setup(t, &c)
	database, err := db.MakeDB(&c)
	require.NoError(t, err)
	registerQueryCounter(t, database)
	ps, err := pubsub.MakePubSub(context.Background(), &c)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &c)
	require.NoError(t, err)
	t.Cleanup(func() {
		_ = ps.Close()
		_ = kvStore.Close()
		if sqlDB, err := database.DB(); err == nil {
			_ = sqlDB.Close()
		}
	})

	seedUser(t, database, listedUser, "N0LIST")
	seedUser(t, database, otherUser, "N0OTHR")
	require.NoError(t, database.Create(&models.UserRadioID{UserID: listedUser, RadioID: listedRadio}).Error)
	seedRepeater(t, database, ruleSender, otherUser)
	seedRepeater(t, database, ruleReceiver, otherUser)
	seedTalkgroup(t, database, 1, "TG1")
	seedTalkgroup(t, database, 2, "TG2")
	for _, rpt := range []uint{ruleSender, ruleReceiver} {
		var r models.Repeater
		require.NoError(t, database.First(&r, rpt).Error)
		require.NoError(t, database.Model(&r).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}, &models.Talkgroup{ID: 2}))
		require.NoError(t, database.Model(&r).Association("TS2StaticTalkgroups").Append(&models.Talkgroup{ID: 3100}))
	}

	e := &rulesEnv{db: database, ps: ps, kv: kvStore, next: 1}
	e.hub = newRulesHub(t, database, ps, kvStore)
	e.srv = e.hub.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	for _, rpt := range []uint{ruleSender, ruleReceiver} {
		e.hub.ActivateRepeater(context.Background(), rpt)
	}
	knowTalkgroups(t, e.hub)
	return e
}

func forRuleBackends(t *testing.T, f func(t *testing.T, e *rulesEnv)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			f(t, rulesSetup(t, be))
		})
	}
}

func (e *rulesEnv) setRules(t *testing.T, rpt uint, kinds []models.RoutingRuleKind, rules ...models.RoutingRule) {
	t.Helper()
	require.NoError(t, models.ReplaceRoutingRules(e.db, rpt, kinds, rules))
	e.hub.ReloadRepeater(context.Background(), rpt)
}

func listedOn(slot uint8) models.RoutingRule {
	return models.RoutingRule{Kind: models.RoutingRuleSlotUser, Slot: slot, SubjectID: listedUser}
}

func blockTG1() models.RoutingRule {
	return models.RoutingRule{Kind: models.RoutingRuleBlockTalkgroup, SubjectID: 1}
}

func allowTG(tg uint) models.RoutingRule {
	return models.RoutingRule{Kind: models.RoutingRuleAllowTalkgroup, SubjectID: tg}
}

type burst struct {
	from, src, dst uint
	slot           bool
	private        bool
}

// carried sends b and then a marker burst from the same source slot, which
// takes the same path, and reports whether the receiver got b before the
// marker.
func (e *rulesEnv) carried(t *testing.T, b burst) bool {
	t.Helper()
	stream := e.next
	e.next += 2
	p := makeVoicePacket(b.dst, stream, !b.private, b.slot)
	p.Repeater, p.Src = b.from, b.src
	marker := makeVoicePacket(e.markerTG(b), stream+1, true, b.slot)
	marker.Repeater, marker.Src = b.from, listedUser
	e.hub.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	e.hub.RoutePacket(context.Background(), marker, models.RepeaterTypeMMDVM)
	got := false
	for {
		select {
		case rp := <-e.srv.Packets:
			if rp.RepeaterID == b.from {
				continue
			}
			require.Equal(t, ruleReceiver, rp.RepeaterID)
			switch rp.Packet.StreamID {
			case stream:
				got = true
			case stream + 1:
				return got
			}
		case <-time.After(failAfter):
			require.FailNow(t, "the marker burst did not arrive")
		}
	}
}

func (e *rulesEnv) markerTG(b burst) uint {
	if b.slot {
		return 3100
	}
	return 2
}

func TestASlotLockCarriesOnlyTheListedUsersIn(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleSender, models.SlotRuleKinds(), listedOn(1))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 1}))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 1}), "a radio ID that the user added")
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: unknownRadio, dst: 1}))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 3100, slot: true}), "the other slot stays open")

		e.setRules(t, ruleSender, models.SlotRuleKinds())
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "an edit takes effect at once")
	})
}

func TestASlotLockCarriesOnlyTheListedUsersOut(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleReceiver, models.SlotRuleKinds(), listedOn(2))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedRadio, dst: 3100, slot: true}))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 3100, slot: true}))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "the other slot stays open")
	})
}

func TestASlotLockCarriesPrivateTrafficToAListedUserOut(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleReceiver, models.SlotRuleKinds(), listedOn(1))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: listedUser, private: true}), "to the account ID")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: unknownRadio, dst: listedRadio, private: true}), "to an added radio ID")
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: unknownRadio, private: true}))
	})
}

func TestASlotLockRefusesPrivateTrafficFromUnlistedRadiosIn(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleSender, models.SlotRuleKinds(), listedOn(1))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: listedUser, private: true}))
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: unknownRadio, private: true}))
	})
}

func TestATalkgroupBlocklistKeepsTheTalkgroupOff(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleReceiver, models.TalkgroupRuleKinds(), blockTG1())
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "out")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 2}))

		e.setRules(t, ruleReceiver, models.TalkgroupRuleKinds())
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), blockTG1())
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "in")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 2}))
	})
}

func TestATalkgroupAllowlistCarriesOnlyItsTalkgroups(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleReceiver, models.TalkgroupRuleKinds(), allowTG(2), allowTG(3100))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "out")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 2}))

		e.setRules(t, ruleReceiver, models.TalkgroupRuleKinds())
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), allowTG(2), allowTG(3100))
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "in")
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 2}))
	})
}

func TestRoutingRulesReadNoDatabasePerBurst(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setRules(t, ruleReceiver, models.SlotRuleKinds(), listedOn(1))
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), blockTG1())
		assert.True(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 2}))
		reads := countTableQueries(t, e.db, "routing_rules")
		for range 10 {
			assert.True(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 2}))
			assert.False(t, e.carried(t, burst{from: ruleSender, src: listedUser, dst: 1}))
		}
		assert.LessOrEqual(t, reads.Load(), int64(2), "40 bursts read the rules at most in a refresh of each repeater")
	})
}

func TestARuleEditOnOneReplicaTakesEffectAtOnceOnAnother(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		other := newRulesHub(t, e.db, e.ps, e.kv)
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
		changed := make(chan struct{}, 1)
		t.Cleanup(e.hub.OnRepeaterChange(func(id uint) {
			if id == ruleReceiver {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}))
		require.NoError(t, models.ReplaceRoutingRules(e.db, ruleReceiver, models.SlotRuleKinds(), []models.RoutingRule{listedOn(1)}))
		other.ReloadRepeater(context.Background(), ruleReceiver)
		select {
		case <-changed:
		case <-time.After(failAfter):
			require.FailNow(t, "the change notice did not arrive")
		}
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
	})
}

func (e *rulesEnv) setAccess(t *testing.T, tg uint, restricted bool, repeaters ...uint) {
	t.Helper()
	require.NoError(t, models.SetTalkgroupAccess(e.db, tg, restricted, repeaters))
	e.hub.TalkgroupChanged(tg)
}

func TestARestrictedTalkgroupReachesOnlyApprovedRepeaters(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setAccess(t, 1, true, ruleSender)
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "out")
		e.setAccess(t, 1, true, ruleReceiver)
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "in")
		e.setAccess(t, 1, true, ruleSender, ruleReceiver)
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
		e.setAccess(t, 1, false)
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}), "an open talkgroup")
	})
}

func TestARestrictedTalkgroupLinksDynamicallyOnlyToApprovedRepeaters(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		e.setAccess(t, 2, true, ruleSender)
		dynamic := func(rpt uint) *uint {
			t.Helper()
			var r models.Repeater
			require.NoError(t, e.db.First(&r, rpt).Error)
			return r.TS1DynamicTalkgroupID
		}
		e.hub.LinkDynamicTalkgroup(context.Background(), ruleReceiver, 2, false)
		assert.Nil(t, dynamic(ruleReceiver))
		e.hub.LinkDynamicTalkgroup(context.Background(), ruleSender, 2, false)
		if assert.NotNil(t, dynamic(ruleSender)) {
			assert.EqualValues(t, 2, *dynamic(ruleSender))
		}
	})
}

func TestATalkgroupAccessEditOnOneReplicaTakesEffectAtOnceOnAnother(t *testing.T) {
	t.Parallel()
	forRuleBackends(t, func(t *testing.T, e *rulesEnv) {
		other := newRulesHub(t, e.db, e.ps, e.kv)
		assert.True(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
		changed := make(chan struct{}, 1)
		t.Cleanup(e.hub.OnTalkgroupChange(func(id uint) {
			if id == 1 {
				select {
				case changed <- struct{}{}:
				default:
				}
			}
		}))
		require.NoError(t, models.SetTalkgroupAccess(e.db, 1, true, []uint{ruleSender}))
		other.TalkgroupChanged(1)
		select {
		case <-changed:
		case <-time.After(failAfter):
			require.FailNow(t, "the change notice did not arrive")
		}
		assert.False(t, e.carried(t, burst{from: ruleSender, src: otherUser, dst: 1}))
	})
}
