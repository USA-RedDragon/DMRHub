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
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// deliveryWait is the longest that a test waits for a burst or a word
// that must come. It bounds only a test that fails.
const deliveryWait = 30 * time.Second

func dataHeader(src, dst, repeater, stream uint) models.Packet {
	return models.Packet{
		Signature:   string(dmrconst.CommandDMRD),
		Src:         src,
		Dst:         dst,
		Repeater:    repeater,
		FrameType:   dmrconst.FrameDataSync,
		DTypeOrVSeq: 6,
		StreamID:    stream,
		BER:         -1,
		RSSI:        -1,
	}
}

func lastSeenHub(t *testing.T) (*hub.Hub, *gorm.DB, *hub.ServerHandle) {
	t.Helper()
	h, database, _, srv := lastSeenHubWithPubSub(t)
	return h, database, srv
}

func lastSeenHubWithPubSub(t *testing.T) (*hub.Hub, *gorm.DB, pubsub.PubSub, *hub.ServerHandle) {
	t.Helper()
	return lastSeenHubWrappingKV(t, func(k kv.KV) kv.KV { return k })
}

func lastSeenHubWrappingKV(t *testing.T, wrapKV func(kv.KV) kv.KV) (*hub.Hub, *gorm.DB, pubsub.PubSub, *hub.ServerHandle) {
	t.Helper()
	h, database, ps := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, wrapKV)
	seedUser(t, database, 1000001, "OTHER")
	seedUser(t, database, 1000002, "TARGET")
	seedRepeater(t, database, 100003, 1000002)
	seedRepeater(t, database, 100004, 1000001)
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), 100003)
	h.ActivateRepeater(context.Background(), 100004)
	return h, database, ps, srv
}

func expectAt(t *testing.T, srv *hub.ServerHandle, repeater uint, what string) {
	t.Helper()
	select {
	case rp := <-srv.Packets:
		assert.Equal(t, repeater, rp.RepeaterID, what)
	case <-time.After(deliveryWait):
		t.Fatalf("%s: nothing delivered", what)
	}
	select {
	case rp := <-srv.Packets:
		t.Fatalf("%s: extra packet to %d", what, rp.RepeaterID)
	case <-time.After(200 * time.Millisecond):
	}
}

func expectNone(t *testing.T, srv *hub.ServerHandle, what string) {
	t.Helper()
	select {
	case rp := <-srv.Packets:
		t.Fatalf("%s: delivered to %d", what, rp.RepeaterID)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestPrivateTrafficFindsRadioHeardOnlyThroughData(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	hear(t, h, 1000002, 100003, 1)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "private data to a radio heard on its owner's repeater")
}

func TestUntrustedSightingDoesNotMoveRadio(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)
	hear(t, h, 1000002, 100003, 1)
	hear(t, h, 1000002, 100004, 2)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "a sighting on another user's hotspot must not move the radio")
}

func TestTrustedRepeaterMovesRadio(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)
	h.ReloadRepeater(context.Background(), 100004)
	hear(t, h, 1000002, 100003, 1)
	hear(t, h, 1000002, 100004, 2)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 3), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100004, "a sighting on a trusted repeater moves the radio")
}

func TestUntrustedCallRowDoesNotLocateRadio(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	userID, repeaterID := uint(1000002), uint(100004)
	require.NoError(t, database.Create(&models.Call{UserID: &userID, RepeaterID: &repeaterID, IngressID: repeaterID, CreatedAt: time.Now()}).Error)
	loadLookups(t, h, nil, []uint{1000002})

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 1), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "a call on another user's untrusted hotspot must not locate the radio")
}

// TestPrivateTrafficFollowsASightingThatIsNotStoredYet answers a radio
// right after it is heard, while a slow database holds the store of the
// sighting.
func TestPrivateTrafficFollowsASightingThatIsNotStoredYet(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	slow := slowDown(t, database)
	loadLookups(t, h, nil, []uint{1000002})
	slow.on.Store(true)
	defer slow.on.Store(false)

	h.RoutePacket(context.Background(), dataHeader(1000002, 1, 100003, 1), models.RepeaterTypeMMDVM)
	require.Eventually(t, func() bool { return h.HeardHere(1000002) == 100003 }, deliveryWait, 10*time.Millisecond, "the hub did not hear the radio")
	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "private traffic goes where the radio was just heard")
}

// TestASlowDatabaseLosesNoSighting hears a radio on its owner's hotspot,
// and another radio on a hotspot of another user, each while every
// statement of the database takes longer than lookup.Wait, so the lookup
// of the owner of each radio gives up. Both sightings must reach the
// record once the database is fast again: the first moves its radio, and
// the second is only the last repeater that heard its radio.
func TestASlowDatabaseLosesNoSighting(t *testing.T) {
	t.Parallel()
	h, database, _ := lastSeenHub(t)
	slow := slowDownBy(t, database, lookup.Wait+200*time.Millisecond)
	ctx := context.Background()
	hearSlowly := func(radio, repeater, stream uint) {
		slow.on.Store(true)
		h.RoutePacket(ctx, dataHeader(radio, 1, repeater, stream), models.RepeaterTypeMMDVM)
		time.Sleep(2 * lookup.Wait)
		slow.on.Store(false)
	}
	heard := func(radio, vetted, last uint) func() bool {
		return func() bool { return h.HeardAt(ctx, radio) == vetted && h.HeardLastOn(ctx, radio) == last }
	}

	hearSlowly(1000002, 100003, 1)
	require.Eventually(t, heard(1000002, 100003, 100003), 30*time.Second, 10*time.Millisecond, "the sighting on the owner's hotspot was lost")
	hearSlowly(1000001, 100003, 2)
	require.Eventually(t, heard(1000001, 0, 100003), 30*time.Second, 10*time.Millisecond, "the sighting on another user's hotspot was lost, or moved the radio")
}

func TestPrivateTrafficToUnregisteredRadioIsDroppedInTheMembersReach(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)
	h.SetConfig(config.Config{DMR: config.DMR{PrivateCalls: config.PrivateCalls{Voice: config.PrivateRoute{Reach: config.PrivateReachMembers}}}})

	hear(t, h, 1234567, 100003, 1)
	h.RoutePacket(context.Background(), dataHeader(1000002, 1234567, 100003, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "private traffic to a radio that is not a user")
}

func TestUntrustingRepeaterStopsRouting(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)
	h.ReloadRepeater(context.Background(), 100004)

	hear(t, h, 1000002, 100004, 1)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", false, 100004).Error)
	h.ReloadRepeater(context.Background(), 100004)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100003, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "private traffic must not follow a sighting on a repeater that is no longer trusted")
}

func TestSightingIsNotHiddenByAnotherSourceOnTheSameStream(t *testing.T) {
	t.Parallel()
	h, _, srv := lastSeenHub(t)

	hear(t, h, 1000001, 100004, 7)
	hear(t, h, 1000002, 100003, 7)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 8), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "a stream ID that another radio used first must not hide a sighting")
}

func TestPrivateStreamKeepsItsTargetWhenTheRadioMoves(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100004).Error)
	h.ReloadRepeater(context.Background(), 100004)

	hear(t, h, 1000002, 100003, 1)
	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the first burst goes where the radio was heard")

	hear(t, h, 1000002, 100004, 3)
	block := dataHeader(1000001, 1000002, 100004, 2)
	block.DTypeOrVSeq = 7
	h.RoutePacket(context.Background(), block, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the rest of the stream goes to the same repeater")

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000002, 100004, 4), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100004, "a new stream goes where the radio is now")
}

// queryCount counts the queries of a table from the time it was made.
type queryCount struct {
	n    *atomic.Int64
	base int64
}

func (c queryCount) Load() int64 {
	return c.n.Load() - c.base
}

// trackerQuery marks the queries of the call tracker of a test hub, which
// the query counters leave out.
type trackerQuery struct{}

// countedQueries holds the query counters of each test database, which
// makeTestHubWrapping registers before the hub runs, as a gorm callback
// may not be registered while other goroutines query.
var countedQueries = xsync.NewMap[*gorm.DB, *xsync.Map[string, *atomic.Int64]]() //nolint:gochecknoglobals // the counters of the databases of all the tests of the package

func registerQueryCounter(t testing.TB, database *gorm.DB) {
	t.Helper()
	tables := xsync.NewMap[string, *atomic.Int64]()
	countedQueries.Store(database, tables)
	t.Cleanup(func() { countedQueries.Delete(database) })
	require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:count_tables", func(tx *gorm.DB) {
		if tx.Statement.Context.Value(trackerQuery{}) != nil {
			return
		}
		n, _ := tables.LoadOrStore(tx.Statement.Table, &atomic.Int64{})
		n.Add(1)
	}))
}

func countTableQueries(t *testing.T, database *gorm.DB, table string) queryCount {
	t.Helper()
	tables, ok := countedQueries.Load(database)
	require.True(t, ok, "the database counts no queries")
	c, _ := tables.LoadOrStore(table, &atomic.Int64{})
	return queryCount{n: c, base: c.Load()}
}

// countedGets counts the reads of key in the KV store.
type countedGets struct {
	kv.KV
	key string
	n   atomic.Int64
}

func (c *countedGets) Get(ctx context.Context, key string) ([]byte, error) {
	if key == c.key {
		c.n.Add(1)
	}
	return c.KV.Get(ctx, key) //nolint:wrapcheck
}

// TestPrivateStreamLooksUpItsTargetOnce sends a private stream to a radio,
// and counts the reads of the owner of the radio in the database and of
// where it was heard in the KV store after the first burst. The stream
// keeps the target of its first burst, so it reads neither again.
func TestPrivateStreamLooksUpItsTargetOnce(t *testing.T) {
	t.Parallel()
	heardReads := &countedGets{key: "dmrhub:lastheard:1000002"}
	h, database, _, srv := lastSeenHubWrappingKV(t, func(k kv.KV) kv.KV {
		heardReads.KV = k
		return heardReads
	})
	loadLookups(t, h, nil, []uint{1000001})

	hear(t, h, 1000002, 100003, 1)

	burst := dataHeader(1000001, 1000002, 100004, 2)
	h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "the first burst of the stream")
	require.Eventually(t, func() bool {
		return h.HeardAt(context.Background(), 1000002) == 100003 && h.HeardAt(context.Background(), 1000001) == 100004
	}, 10*time.Second, 10*time.Millisecond, "the sightings of both streams are recorded")

	users := countTableQueries(t, database, "users")
	heard := heardReads.n.Load()
	for seq := uint(1); seq < 10; seq++ {
		burst.Seq = seq
		h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
		expectAt(t, srv, 100003, "a later burst of the stream")
	}
	assert.Zero(t, users.Load(), "user lookups after the first burst of a private stream")
	assert.Equal(t, heard, heardReads.n.Load(), "reads of where the target was heard after the first burst of a private stream")
}

func TestGroupStreamChecksItsTalkgroupOnce(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100002, 1000001)
	seedTalkgroup(t, database, 1, "TG1")
	var rpt models.Repeater
	require.NoError(t, database.First(&rpt, 100002).Error)
	require.NoError(t, database.Model(&rpt).Association("TS1StaticTalkgroups").Append(&models.Talkgroup{ID: 1}))
	srv := h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), 100002)
	time.Sleep(100 * time.Millisecond)

	burst := makeVoicePacket(1, 55555, true, false)
	h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100002, "the first burst of the stream")

	talkgroups := countTableQueries(t, database, "talkgroups")
	for seq := uint(1); seq < 10; seq++ {
		burst.Seq = seq
		h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
		expectAt(t, srv, 100002, "a later burst of the stream")
	}
	assert.Zero(t, talkgroups.Load(), "talkgroup checks after the first burst of a group stream")
}

func TestStreamListsPeersOnce(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedTalkgroup(t, database, 1, "TG1")
	h.RegisterServer(hub.ServerConfig{Name: "openbridge", Role: hub.RolePeer})
	t.Cleanup(func() { h.UnregisterServer("openbridge") })
	peers := countTableQueries(t, database, "peers")
	burst := makeVoicePacket(1, 55555, true, false)
	h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	require.Eventually(t, func() bool { return peers.Load() > 0 }, deliveryWait, 10*time.Millisecond)
	time.Sleep(200 * time.Millisecond)
	first := peers.Load()
	for seq := uint(1); seq < 10; seq++ {
		burst.Seq = seq
		h.RoutePacket(context.Background(), burst, models.RepeaterTypeMMDVM)
	}
	time.Sleep(500 * time.Millisecond)
	assert.Equal(t, first, peers.Load(), "peer lookups after the first burst of a stream")
}

func addRadioID(t *testing.T, database *gorm.DB, userID, radioID uint) {
	t.Helper()
	require.NoError(t, database.Create(&models.UserRadioID{UserID: userID, RadioID: radioID, VerifiedAt: time.Now()}).Error)
}

func TestPrivateTrafficReachesARadioIDThatAUserAdded(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	addRadioID(t, database, 1000002, 1000005)

	hear(t, h, 1000005, 100003, 1)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000005, 100004, 2), models.RepeaterTypeMMDVM)
	expectAt(t, srv, 100003, "private data to an added radio ID heard on its owner's hotspot")
}

func TestAddedRadioIDHeardOnAnotherUsersHotspotIsNotRouted(t *testing.T) {
	t.Parallel()
	h, database, srv := lastSeenHub(t)
	addRadioID(t, database, 1000002, 1000005)

	hear(t, h, 1000005, 100004, 1)

	h.RoutePacket(context.Background(), dataHeader(1000001, 1000005, 100003, 2), models.RepeaterTypeMMDVM)
	expectNone(t, srv, "an added radio ID heard on a hotspot its owner does not own")
}
