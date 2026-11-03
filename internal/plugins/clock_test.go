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

package plugins_test

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheServiceNotesTheTimesOfAPluginOnItsClock(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Date(2030, 1, 2, 3, 4, 5, 0, time.UTC)
		fake := clock.NewFake(start)
		r := newRig(t, plugins.WithClock(fake))
		p, token := r.add("Parrot", rng(9990, 9990))
		_, err := recv(t, r.connect(token))
		require.NoError(t, err)
		got, err := models.FindPlugin(r.db, p.ID)
		require.NoError(t, err)
		require.NotNil(t, got.LastConnectedAt)
		assert.True(t, start.Equal(*got.LastConnectedAt), "connected at %v", got.LastConnectedAt)
		assert.True(t, got.Online(fake.Now()))

		bubble.SettleDB(t, r.db, time.Minute)
		fake.Advance(plugins.HeartbeatEvery)
		bubble.SettleDB(t, r.db, time.Minute)
		got, err = models.FindPlugin(r.db, p.ID)
		require.NoError(t, err)
		require.NotNil(t, got.LastSeenAt)
		assert.True(t, start.Add(plugins.HeartbeatEvery).Equal(*got.LastSeenAt), "seen at %v", got.LastSeenAt)
		assert.True(t, got.Online(fake.Now()))
		assert.False(t, got.Online(fake.Now().Add(models.PluginOnline)), "offline once the heartbeats stop")
	})
}
