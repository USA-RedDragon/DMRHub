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

// Package monotime keeps a time in an int64 with its monotonic clock

package hub_test

import (
	"context"
	"database/sql"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func dynamicTS1(t *testing.T, database *gorm.DB) *uint {
	t.Helper()
	rpt, err := models.FindRepeaterByID(database, 100001)
	require.NoError(t, err)
	return rpt.TS1DynamicTalkgroupID
}

func dynamicHub(t *testing.T, timeout time.Duration) (*hub.Hub, *gorm.DB) {
	t.Helper()
	h, database := makeTestHub(t)
	settleDB(t, database)
	h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{DynamicTimeout: timeout}}})
	seedUser(t, database, 1000001, "TESTUSER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 2, "TG2")
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	t.Cleanup(func() { h.UnregisterServer(models.RepeaterTypeMMDVM) })
	h.ActivateRepeater(context.Background(), 100001)
	return h, database
}

func linkDynamic(t *testing.T, timeout time.Duration) (*hub.Hub, *gorm.DB) {
	t.Helper()
	h, database := dynamicHub(t, timeout)
	call(t, h, database, 99999)
	require.NotNil(t, dynamicTS1(t, database), "a call on a talkgroup links it")
	return h, database
}

func route(h *hub.Hub, stream uint) {
	h.RoutePacket(context.Background(), makeVoicePacket(2, stream, true, false), models.RepeaterTypeMMDVM)
	h.RoutePacket(context.Background(), makeVoiceTermPacket(2, stream, true, false), models.RepeaterTypeMMDVM)
}

func call(t *testing.T, h *hub.Hub, database *gorm.DB, stream uint) {
	t.Helper()
	route(h, stream)
	settleDB(t, database)
}

// holdWrites takes the write lock of the SQLite file of database on a
// connection of its own, as a writer that the hub does not know of, and
// returns a func that lets it go.
func holdWrites(t *testing.T, database *gorm.DB) func() {
	t.Helper()
	var file string
	require.NoError(t, database.Raw("SELECT file FROM pragma_database_list WHERE name = 'main'").Scan(&file).Error)
	other, err := sql.Open("sqlite", file)
	require.NoError(t, err)
	t.Cleanup(func() { _ = other.Close() })
	conn, err := other.Conn(context.Background())
	require.NoError(t, err)
	_, err = conn.ExecContext(context.Background(), "BEGIN IMMEDIATE")
	require.NoError(t, err)
	return func() {
		_, err := conn.ExecContext(context.Background(), "COMMIT")
		require.NoError(t, err)
		require.NoError(t, conn.Close())
	}
}

// TestADynamicLinkThatWaitsForTheDatabaseIsSettled routes a call while
// another connection holds the write lock of the database, so that the
// link waits in the busy handler of SQLite, whose sleeps durably block a
// goroutine on the virtual clock. After the lock goes, the wait of the
// tests must last until the link is written.
func TestADynamicLinkThatWaitsForTheDatabaseIsSettled(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := dynamicHub(t, 0)
		release := holdWrites(t, database)
		route(h, 99999)
		synctest.Wait()
		release()
		settleDB(t, database)
		require.NotNil(t, dynamicTS1(t, database), "a call on a talkgroup links it once the database lets the write go")
	})
}

func TestADynamicTalkgroupUnlinksAtItsTimeout(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const timeout = 10 * time.Minute
		_, database := linkDynamic(t, timeout)
		time.Sleep(timeout - time.Second)
		settleDB(t, database)
		require.NotNil(t, dynamicTS1(t, database), "the link lasts for the timeout")
		time.Sleep(time.Second + time.Second)
		settleDB(t, database)
		assert.Nil(t, dynamicTS1(t, database), "the link ends within a sweep after the timeout")
	})
}

func TestADynamicTalkgroupTimeoutRestartsAtEachCall(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const timeout = 10 * time.Minute
		h, database := linkDynamic(t, timeout)
		for i := range uint(4) {
			time.Sleep(timeout * 3 / 4)
			call(t, h, database, 100000+i)
			require.NotNil(t, dynamicTS1(t, database), "a call on the dynamic talkgroup restarts its timeout")
		}
		time.Sleep(timeout + time.Second + time.Second)
		settleDB(t, database)
		assert.Nil(t, dynamicTS1(t, database), "the link ends a timeout after the last call")
	})
}

func TestADynamicTalkgroupWithNoTimeoutStays(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := linkDynamic(t, 0)
		time.Sleep(time.Hour)
		settleDB(t, database)
		require.NotNil(t, dynamicTS1(t, database), "a timeout of 0 never unlinks")
		h.SetConfig(config.Config{DMR: config.DMR{Routing: config.Routing{DynamicTimeout: time.Minute}}})
		time.Sleep(time.Minute + 2*time.Second)
		settleDB(t, database)
		assert.Nil(t, dynamicTS1(t, database), "a timeout set while the hub runs applies to the links that exist")
	})
}

func TestADynamicTimeoutTakesEffectWhenTheConfigIsSet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		const timeout = 100 * time.Millisecond
		_, database := linkDynamic(t, timeout)
		time.Sleep(timeout + 200*time.Millisecond)
		settleDB(t, database)
		assert.Nil(t, dynamicTS1(t, database), "a link past the timeout goes soon after the config sets the timeout")
	})
}
