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
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestEphemeralTTLTakesEffectWhenTheConfigIsSet(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		time.Sleep(300 * time.Millisecond)
		old := time.Now().Add(-time.Hour)
		require.NoError(t, database.Create(&models.Talkgroup{ID: 3999, Name: "TG 3999", AutoCreated: true, LastCallAt: &old}).Error)
		cfg := config.Config{}
		cfg.DMR.Routing = config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: time.Second}
		h.SetConfig(config.NewCurrent(&cfg))
		bubble.Eventually(t, func() bool {
			exists, err := models.TalkgroupIDExists(database, 3999)
			return err == nil && !exists
		}, 5*time.Second, 20*time.Millisecond, "a talkgroup past the TTL goes soon after the config turns ephemeral talkgroups on")
	})
}

// TestEphemeralSweepDueDuringASweepRuns moves the clock of the hub past the
// TTL of a talkgroup while a sweep of the ephemeral talkgroups waits on the
// database, with the time of before the move. The sweep that fell due
// meanwhile must run after it and delete the talkgroup, also when the clock
// does not move again.
func TestEphemeralSweepDueDuringASweepRuns(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		fake := clock.NewFake(time.Now())
		h, database, _ := makeTestHubWrappingKV(t, keepPubSub, keepKV, fake)
		now := fake.Now()
		require.NoError(t, database.Create(&models.Talkgroup{ID: 3999, Name: "TG 3999", AutoCreated: true, LastCallAt: &now}).Error)
		var gate atomic.Pointer[chan struct{}]
		entered := make(chan struct{}, 1)
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:hold_sweep", func(tx *gorm.DB) {
			g := gate.Load()
			if g == nil || !strings.Contains(tx.Statement.SQL.String(), "auto_created") {
				return
			}
			select {
			case entered <- struct{}{}:
			default:
			}
			<-*g
		}))
		held := make(chan struct{})
		gate.Store(&held)
		cfg := config.Config{}
		cfg.DMR.Routing = config.Routing{AutoCreate: true, Ephemeral: true, EphemeralTTL: time.Second}
		h.SetConfig(config.NewCurrent(&cfg))
		<-entered
		fake.Advance(2 * time.Second)
		gate.Store(nil)
		close(held)
		bubble.Eventually(t, func() bool {
			exists, err := models.TalkgroupIDExists(database, 3999)
			return err == nil && !exists
		}, 5*time.Second, 20*time.Millisecond, "the sweep that fell due during a sweep deletes the talkgroup past its TTL")
	})
}
