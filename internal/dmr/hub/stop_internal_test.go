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

package hub

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestStopWaitsForTheLoopsOfTheHub stops a hub while its loop of codec
// changes reads the database. Stop must return only after the loop ends,
// since the caller closes the database next, and a read after that fails
// or, on a SQLite file, waits for the lock of a closing connection.
func TestStopWaitsForTheLoopsOfTheHub(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		ps, err := pubsub.MakePubSub(context.Background(), &cfg)
		require.NoError(t, err)
		t.Cleanup(func() {
			_ = ps.Close()
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
		})

		var hold atomic.Bool
		var once sync.Once
		entered, release := make(chan struct{}), make(chan struct{})
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("hold_codecs", func(tx *gorm.DB) {
			if hold.Load() && strings.Contains(tx.Statement.SQL.String(), "ts1_codec") {
				once.Do(func() {
					close(entered)
					<-release
				})
			}
		}))

		h := NewHub(database, nil, ps, nil)
		hold.Store(true)
		require.NoError(t, ps.Publish(codecChangedTopic, []byte("another-replica:100001")))
		<-entered

		stopped := make(chan struct{})
		go func() {
			h.Stop(context.Background())
			close(stopped)
		}()
		synctest.Wait()
		select {
		case <-stopped:
			t.Error("Stop returned while a loop of the hub read the database")
		default:
		}
		close(release)
		<-stopped
	})
}

// TestStopWaitsForTheJobsOfTheOutboxes stops a hub while a job of each of
// its outboxes runs, as the vetting of a sighting reads the database on
// the outbox of the background work. Stop must return only after the job
// ends.
func TestStopWaitsForTheJobsOfTheOutboxes(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		add  func(h *Hub, job outbox.Job)
	}{
		{name: "background work", add: func(h *Hub, job outbox.Job) { h.later.Add(laterKey{what: sightingsWork, id: 1000001}, job) }},
		{name: "bursts", add: func(h *Hub, job outbox.Job) { h.outbox.Add(sourceSlot{}, job) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				h := NewHub(nil, nil, nil, nil)
				entered, release := make(chan struct{}), make(chan struct{})
				tc.add(h, outbox.Job{Topic: "test", Send: func() error {
					close(entered)
					<-release
					return nil
				}})
				<-entered
				stopped := make(chan struct{})
				go func() {
					h.Stop(context.Background())
					close(stopped)
				}()
				synctest.Wait()
				select {
				case <-stopped:
					t.Error("Stop returned while a job of the outbox ran")
				default:
				}
				close(release)
				<-stopped
			})
		})
	}
}

// TestStopWaitsForTheRefreshOfARepeater stops a hub while it reads a
// repeater again whose cache entry is older than repeaterCacheTTL, on a
// goroutine of its own. Stop must return only after the read ends.
func TestStopWaitsForTheRefreshOfARepeater(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		t.Cleanup(func() {
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
		})
		require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Approved: true}).Error)
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001, Callsign: "T0001ST"},
			OwnerID:               1000001,
			Type:                  models.RepeaterTypeMMDVM,
		}).Error)

		var hold atomic.Bool
		var once sync.Once
		entered, release := make(chan struct{}), make(chan struct{})
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("hold_repeater", func(tx *gorm.DB) {
			if hold.Load() && tx.Statement.Table == "repeaters" {
				once.Do(func() {
					close(entered)
					<-release
				})
			}
		}))

		h := NewHub(database, nil, nil, nil)
		_, err = h.waitRepeater(100001)
		require.NoError(t, err)
		time.Sleep(repeaterCacheTTL)
		hold.Store(true)
		_, err = h.waitRepeater(100001)
		require.NoError(t, err)
		<-entered

		stopped := make(chan struct{})
		go func() {
			h.Stop(context.Background())
			close(stopped)
		}()
		synctest.Wait()
		select {
		case <-stopped:
			t.Error("Stop returned while the hub read a repeater again")
		default:
		}
		close(release)
		<-stopped
	})
}

// TestStopEndsAFeedThatNoSubscriptionHolds starts the feed of a repeater
// that no subscription holds, as an activation that races Stop leaves
// after Stop deactivated the repeaters. Stop waits for the reader of the
// feed, so the reader must end when the hub stops.
func TestStopEndsAFeedThatNoSubscriptionHolds(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		ps, err := pubsub.MakePubSub(context.Background(), &cfg)
		require.NoError(t, err)
		t.Cleanup(func() { _ = ps.Close() })
		h := NewHub(nil, nil, ps, nil)
		h.subscriptionMgr.mu.Lock()
		h.subscriptionMgr.feedOf(context.Background(), 100001)
		h.subscriptionMgr.mu.Unlock()
		h.Stop(context.Background())
	})
}

// TestStopEndsTheDatabaseReadsOfTheHub stops a hub while it reads a
// repeater again, on a goroutine that Stop waits for, from a database that
// answers only when the context of the read ends, as a stalled database
// does for a driver that aborts a query on its context. Stop must end the
// read and return, and the read must fail.
func TestStopEndsTheDatabaseReadsOfTheHub(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		cfg, err := configulator.New(config.ConfigSchema()).Default()
		require.NoError(t, err)
		memdb.Use(t, &cfg)
		database, err := db.MakeDB(&cfg)
		require.NoError(t, err)
		t.Cleanup(func() {
			sqlDB, _ := database.DB()
			_ = sqlDB.Close()
		})
		require.NoError(t, database.Create(&models.User{ID: 1000001, Callsign: "N0CALL", Username: "n0call", Approved: true}).Error)
		require.NoError(t, database.Create(&models.Repeater{
			RepeaterConfiguration: models.RepeaterConfiguration{ID: 100001, Callsign: "T0001ST"},
			OwnerID:               1000001,
			Type:                  models.RepeaterTypeMMDVM,
		}).Error)

		var stall atomic.Bool
		stalled := make(chan error, 1)
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("stall_repeater", func(tx *gorm.DB) {
			if stall.Load() && tx.Statement.Table == "repeaters" {
				<-tx.Statement.Context.Done()
				_ = tx.AddError(tx.Statement.Context.Err())
				stalled <- tx.Statement.Context.Err()
			}
		}))

		h := NewHub(database, nil, nil, nil)
		_, err = h.waitRepeater(100001)
		require.NoError(t, err)
		time.Sleep(repeaterCacheTTL)
		stall.Store(true)
		_, err = h.waitRepeater(100001)
		require.NoError(t, err)
		synctest.Wait()

		h.Stop(context.Background())
		require.ErrorIs(t, <-stalled, context.Canceled, "Stop must end the read of the hub")
	})
}

type stalledKV struct {
	kv.KV
	called chan struct{}
}

func (s stalledKV) Get(ctx context.Context, _ string) ([]byte, error) {
	close(s.called)
	<-ctx.Done()
	return nil, fmt.Errorf("stalled: %w", ctx.Err())
}

// TestStopEndsTheKVCallsOfTheHub reads the KV store of a hub with a
// context that never ends, from a store that answers only when the context
// of the call ends. Stop must end the call.
func TestStopEndsTheKVCallsOfTheHub(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		store := stalledKV{called: make(chan struct{})}
		h := NewHub(nil, store, nil, nil)
		got := make(chan error, 1)
		go func() {
			_, err := h.kv.Get(context.Background(), "key")
			got <- err
		}()
		<-store.called
		h.Stop(context.Background())
		require.ErrorIs(t, <-got, context.Canceled, "Stop must end the KV call of the hub")
	})
}
