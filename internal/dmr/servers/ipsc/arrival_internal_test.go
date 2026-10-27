//go:build linux

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

package ipsc

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestAPeerCallIsTimedFromWhenItsBurstsCame hands the server the bursts of
// a voice call of an XPR 8400 all at once, as a reader that runs late
// takes them from the socket, each with the time the kernel got it. The
// call tracker must time the call from those times, and not from when the
// hub got the bursts.
func TestAPeerCallIsTimedFromWhenItsBurstsCame(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.Database.Database = fmt.Sprintf("file:ipsc_arrival_%d", time.Now().UnixNano())
	cfg.Database.ExtraParameters = []string{"mode=memory", "cache=shared"}
	database, err := db.MakeDB(&cfg)
	require.NoError(t, err)
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	ps, err := pubsub.MakePubSub(context.Background(), &cfg)
	require.NoError(t, err)
	ct := calltracker.NewCallTracker(database, ps, kvStore)
	h := hub.NewHub(database, kvStore, ps, ct)
	t.Cleanup(func() {
		h.Stop(context.Background())
		ct.Stop()
		_ = ps.Close()
		_ = kvStore.Close()
	})
	call := readXPRCapture(t, "voicecall")
	require.NoError(t, database.Create(&models.Repeater{
		RepeaterConfiguration: models.RepeaterConfiguration{ID: uint(decodeUserT(t, call[0]).ID)},
		Type:                  models.RepeaterTypeIPSC,
	}).Error)
	require.NoError(t, database.Create(&models.Talkgroup{ID: uint(decodeUserT(t, call[0]).Destination)}).Error)
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeIPSC, Role: hub.RoleRepeater})

	s, srv := newTestServerWithUDP(t)
	s.hub = h
	s.SetBurstHandler(s.peerBursts(t.Context()))
	_, addr := dialServer(t, srv)

	const period = 60 * time.Millisecond
	came := time.Now().Add(-time.Duration(len(call)) * period)
	for i, raw := range call {
		require.NoError(t, s.handlePacketAt(t.Context(), signTestPacket(t, raw), addr, came.Add(time.Duration(i)*period)))
	}

	var calls []models.Call
	require.Eventually(t, func() bool {
		calls = nil
		return database.Find(&calls).Error == nil && len(calls) == 1 && !calls[0].Active
	}, 10*time.Second, 10*time.Millisecond, "the call tracker did not keep and end the call")
	assert.InDelta(t, time.Duration(len(call)-1)*period, calls[0].Duration, float64(period), "the call must last from its first burst to its terminator")
}
