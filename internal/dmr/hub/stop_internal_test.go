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
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/outbox"
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
