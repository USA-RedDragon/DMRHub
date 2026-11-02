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

package modem

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/memdb"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// feedWatch is a pubsub whose feeds count how many the hub made and
// closed, as the hub makes one when it activates a repeater and closes it
// when it deactivates the repeater.
type feedWatch struct {
	pubsub.PubSub
	made, closed atomic.Int32
}

func (w *feedWatch) Feed() pubsub.Feed {
	w.made.Add(1)
	return &watchedFeed{Feed: w.PubSub.Feed(), w: w}
}

type watchedFeed struct {
	pubsub.Feed
	w *feedWatch
}

func (f *watchedFeed) Close() error {
	f.w.closed.Add(1)
	return f.Feed.Close() //nolint:wrapcheck
}

// TestAModemThatStopsWhileItsQueueIsFullIsDeactivated stops a modem while
// its job queue is full of jobs that a slow store holds. The hub must
// still deactivate the repeater of the modem once the queue moves.
func TestAModemThatStopsWhileItsQueueIsFullIsDeactivated(t *testing.T) {
	t.Parallel()
	const id = uint32(990401)
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	memdb.Use(t, &cfg)
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(id)},
		Type:                  models.RepeaterTypeModem,
	}).Error)
	kvStore, err := kv.MakeKV(context.Background(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	w := &feedWatch{PubSub: ps}
	h := hub.NewHub(database, kvStore, w, nil)
	t.Cleanup(func() {
		h.Stop(context.Background())
		_ = ps.Close()
		_ = kvStore.Close()
	})
	s := &Server{id: id, hub: h, ctx: context.Background()}
	s.activate()
	s.jobs.Wait()
	require.Equal(t, int32(1), w.made.Load(), "the hub did not activate the repeater of the modem")

	release, running := make(chan struct{}), make(chan struct{})
	require.True(t, s.jobs.Do(uint(id), func() {
		close(running)
		<-release
	}))
	<-running
	for s.jobs.Do(uint(id), func() {}) {
	}
	var once sync.Once
	free := func() { once.Do(func() { close(release) }) }
	t.Cleanup(free)
	s.deactivate()
	free()
	s.jobs.Wait()
	assert.Equal(t, int32(1), w.closed.Load(), "the hub did not deactivate the repeater of the modem")
}
