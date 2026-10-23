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

package messages_test

import (
	"context"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/messages"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/retention"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/packet"
	"github.com/USA-RedDragon/dmrgo/v2/sms"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newEnvOn(t *testing.T, be testutils.Backend) (*env, *config.Config) {
	t.Helper()
	var cfg *config.Config
	router, tdb, err := testutils.CreateTestDBRouterWithOptions(func(c *config.Config) {
		be.Setup(t, c)
		cfg = c
	})
	require.NoError(t, err)
	t.Cleanup(tdb.CloseDB)
	db := tdb.DB()
	testutils.SeedAccount(t, db, owner, "KI5VMF", "owner")
	testutils.SeedAccount(t, db, other, "KD5SMP", "other")
	require.NoError(t, db.Create(&models.Talkgroup{ID: tg, Name: "Texas"}).Error)
	return &env{t: t, router: router, db: db, jar: testutils.Login(t, router, "owner"), ps: tdb.PubSub()}, cfg
}

func backends() []testutils.Backend {
	return []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend()}
}

func smsBursts(t *testing.T, text *sms.Text) []layer2.RawBurst {
	t.Helper()
	var h sms.Header
	payload, err := sms.AppendPacket(nil, text, &h)
	require.NoError(t, err)
	p, err := packet.NewPacket(h.Data, payload, enums.DataTypeRate12)
	require.NoError(t, err)
	bursts, err := packet.Bursts(&p, 1, layer2.SyncSourceMS)
	require.NoError(t, err)
	return bursts
}

func TestRoutedSMSReachesTheInboxOnlyAfterOptIn(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e, cfg := newEnvOn(t, be)
			ps, err := pubsub.MakePubSub(context.TODO(), cfg)
			require.NoError(t, err)
			kvStore, err := kv.MakeKV(context.TODO(), cfg)
			require.NoError(t, err)
			ct := calltracker.NewCallTracker(e.db, ps, kvStore)
			store := messages.NewStore(e.db, kvStore)
			ct.SetDataObserver(store)
			t.Cleanup(func() {
				ct.Stop()
				store.Stop()
				_ = ps.Close()
				_ = kvStore.Close()
			})
			send := func(stream uint, body string) {
				for i, b := range smsBursts(t, &sms.Text{Format: sms.FormatIP, Source: uint32(other), Destination: uint32(owner), Body: []byte(body)}) {
					ct.Track(context.Background(), models.Packet{
						Signature: string(dmrconst.CommandDMRD), Seq: uint(i), Src: other, Dst: owner, Repeater: 100001,
						Slot: true, FrameType: dmrconst.FrameDataSync, DTypeOrVSeq: uint(b.SlotType().DataType), StreamID: stream, DMRData: b, BER: -1, RSSI: -1,
					}, models.RepeaterTypeMMDVM, false)
				}
			}
			stored := func(n int64) {
				require.Eventually(t, func() bool {
					ct.Flush()
					store.Flush()
					var c int64
					require.NoError(t, e.db.Model(&models.Message{}).Count(&c).Error)
					return c == n
				}, 8*time.Second, 50*time.Millisecond)
			}

			send(8001, "while off")
			stored(1)
			assert.Equal(t, 0, e.list("/api/v1/messages/inbox").Total, "the hub stored it, the user does not see it")

			e.storage(true)
			time.Sleep(10 * time.Millisecond)
			send(8002, "while on")
			stored(2)
			l := e.list("/api/v1/messages/inbox")
			assert.Equal(t, []string{"while on"}, texts(l))
			admin := testutils.Login(t, e.router, "Admin")
			assert.Equal(t, int64(2), e.admin(admin, "").Total)
		})
	}
}

func TestPrivacyOnEveryBackend(t *testing.T) {
	t.Parallel()
	for _, be := range backends() {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			e, _ := newEnvOn(t, be)
			before := e.msg(time.Now().Add(-time.Hour), other, owner, false, "before")
			since := e.storage(true)
			rows := make([]models.Message, 0, 260)
			for i := range 260 {
				text := fmt.Sprintf("m %03d", i)
				rows = append(rows, models.Message{ReceivedAt: since.Add(time.Duration(i+1) * time.Millisecond), SourceID: other, DestinationID: owner, Format: models.MessageFormatIP, Text: &text})
			}
			require.NoError(t, e.db.CreateInBatches(rows, 100).Error)
			notMine := e.msg(since.Add(time.Second), other, uint(1000099), false, "not mine")

			l := e.list("/api/v1/messages/inbox?limit=50")
			assert.Equal(t, 250, l.Total)
			assert.Equal(t, "m 259", *l.Messages[0].Text)
			assert.Equal(t, http.StatusNotFound, e.get(before.ID))
			assert.Equal(t, http.StatusNotFound, e.get(notMine.ID))
			assert.Equal(t, http.StatusNotFound, e.get(rows[9].ID), "row 251 is past the cap")
			assert.Equal(t, http.StatusOK, e.get(rows[10].ID))
			assert.Equal(t, 250, e.stats().Inbox)
		})
	}
}

func TestRetentionPurgeEmptiesAdminAndUserViews(t *testing.T) {
	t.Parallel()
	e, cfg := newEnvOn(t, testutils.SQLiteMemoryBackend())
	since := e.storage(true)
	e.msg(since.Add(time.Millisecond), other, owner, false, "keep me")
	old := e.msg(since.Add(time.Millisecond), other, owner, false, "old")
	require.NoError(t, e.db.Model(&models.Message{}).Where("id = ?", old.ID).UpdateColumn("received_at", time.Now().Add(-40*24*time.Hour).UTC()).Error)
	require.NoError(t, e.db.Exec("UPDATE users SET sms_storage_since = ? WHERE id = ?", time.Now().Add(-50*24*time.Hour).UTC(), owner).Error)
	admin := testutils.Login(t, e.router, "Admin")
	require.Equal(t, int64(2), e.admin(admin, "").Total)
	require.Equal(t, 2, e.list("/api/v1/messages/inbox").Total)

	kvStore, err := kv.MakeKV(context.Background(), cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })
	purgeCfg := *cfg
	purgeCfg.DMR.Retention.Messages = 30
	res, err := retention.NewPurger(&purgeCfg, e.db, kvStore).Run(context.Background(), time.Now())
	require.NoError(t, err)
	assert.Equal(t, 1, res.Messages)

	assert.Equal(t, int64(1), e.admin(admin, "").Total)
	assert.Equal(t, []string{"keep me"}, texts(e.list("/api/v1/messages/inbox")))
	assert.Equal(t, http.StatusNotFound, e.get(old.ID))
}
