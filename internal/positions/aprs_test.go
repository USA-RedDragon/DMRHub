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

package positions_test

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/aprs"
	"github.com/USA-RedDragon/DMRHub/internal/aprs/aprstest"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/positions"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// passcode is the APRS-IS passcode of N0HUB, the login of the gateway.
const passcode = "14215"

// testClock reads a time that the test moves, and runs the real timers.
type testClock struct {
	clock.Clock
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

type gateway struct {
	t      *testing.T
	db     *gorm.DB
	ps     pubsub.PubSub
	kv     kv.KV
	cfg    *config.Config
	srv    *aprstest.Server
	pusher *positions.Pusher
	clock  *testClock
}

func newGateway(t *testing.T) *gateway {
	t.Helper()
	database, ps, pusher := rig(t)
	srv := aprstest.NewServer(t, passcode)
	require.True(t, strings.HasPrefix(srv.Addr(), "127.0.0.1:"), "tests send only to loopback")
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	cfg.DMR.Positions.APRSIS = config.APRSIS{Callsign: "N0HUB-10", Server: srv.Addr()}
	kvStore, err := kv.MakeKV(context.Background(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = kvStore.Close() })
	require.NoError(t, database.Create(&models.Repeater{RepeaterConfiguration: models.RepeaterConfiguration{ID: repeater, Callsign: "W5ABC"}, OwnerID: owner}).Error)
	require.NoError(t, models.SetPositionOptions(database, owner, owner, models.PositionOptions{
		Interval: 10 * time.Minute, Retention: 24 * time.Hour, APRS: true, APRSSSID: "9", APRSComment: "DMR 3191868",
	}))
	return &gateway{t: t, db: database, ps: ps, kv: kvStore, cfg: &cfg, srv: srv, pusher: pusher, clock: &testClock{Clock: clock.Real(), now: time.Now()}}
}

type watch struct {
	refreshed chan bool
	handled   chan handled
	logins    chan bool
}

type handled struct {
	p    models.Position
	sent bool
}

func (g *gateway) open() (*positions.Gateway, *watch) {
	g.t.Helper()
	w := &watch{refreshed: make(chan bool, 1), handled: make(chan handled, 32), logins: make(chan bool, 16)}
	gw := positions.NewGateway(g.db, g.cfg, g.kv, g.ps, "1.2.3")
	positions.SetClock(gw, 50*time.Millisecond, g.clock)
	positions.Observe(gw, func(connected bool) {
		select {
		case w.refreshed <- connected:
		default:
		}
	}, func(p models.Position, sent bool) { w.handled <- handled{p, sent} }, func(verified bool) { w.logins <- verified })
	gw.Start(context.Background())
	g.t.Cleanup(gw.Stop)
	return gw, w
}

func next[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), aprs.ReadTimeout)
	defer cancel()
	select {
	case v := <-ch:
		return v
	case <-ctx.Done():
		t.Fatalf("no %s within %s, the read timeout of the APRS-IS client", what, aprs.ReadTimeout)
		var zero T
		return zero
	}
}

func (g *gateway) start() (*positions.Gateway, *watch) {
	g.t.Helper()
	gw, w := g.open()
	require.True(g.t, next(g.t, w.logins, "login reply"), "the server did not verify the login")
	return gw, w
}

func (g *gateway) lines(n int) []string {
	g.t.Helper()
	ctx, cancel := context.WithTimeout(g.t.Context(), aprs.ReadTimeout)
	defer cancel()
	return g.srv.WaitLines(ctx, n)
}

func (w *watch) sent(t *testing.T) bool {
	t.Helper()
	return next(t, w.handled, "handled position").sent
}

func (g *gateway) at(lat, lon float64) models.Position {
	p := position()
	p.Latitude, p.Longitude, p.ReceivedAt = lat, lon, g.clock.Now()
	return p
}

func TestTheGatewaySendsATrackedRadioUnderItsOwnersCallsign(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	_, w := g.start()
	p := g.at(30.2672, -97.7431)
	speed, heading := 4.0, 45.0
	p.SpeedMPS, p.HeadingDeg = &speed, &heading
	g.pusher.Push(p)
	require.True(t, w.sent(t))
	assert.Equal(t, []string{"KI5VMF-9>APZDMH,W5ABC*,qAR,N0HUB-10:!3016.03N/09744.59W[045/008DMR 3191868"}, g.lines(1))
}

func TestTheOwnersSymbolReplacesTheServices(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	require.NoError(t, models.SetPositionOptions(g.db, owner, owner, models.PositionOptions{
		Interval: 10 * time.Minute, Retention: 24 * time.Hour, APRS: true, APRSSymbol: "bike",
	}))
	_, w := g.start()
	g.pusher.Push(g.at(30.2672, -97.7431))
	require.True(t, w.sent(t))
	assert.Equal(t, []string{"KI5VMF>APZDMH,W5ABC*,qAR,N0HUB-10:!3016.03N/09744.59Wb"}, g.lines(1))
}

func TestAtMostOnePacketPerRadioEachMinuteAndOnlyWhenItMoved(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	_, w := g.start()
	acc := 50.0
	first := g.at(30.2672, -97.7431)
	first.AccuracyM = &acc
	g.pusher.Push(first)
	require.True(t, w.sent(t))
	require.Len(t, g.lines(1), 1)

	g.clock.Add(30 * time.Second)
	g.pusher.Push(g.at(30.30, -97.7431))
	assert.False(t, w.sent(t), "too soon")
	g.clock.Add(31 * time.Second)
	near := g.at(30.2675, -97.7431)
	near.AccuracyM = &acc
	g.pusher.Push(near)
	assert.False(t, w.sent(t), "within the accuracy")

	g.pusher.Push(g.at(30.30, -97.7431))
	require.True(t, w.sent(t))
	lines := g.lines(2)
	require.Len(t, lines, 2)
	assert.Contains(t, lines[1], "!3018.00N/")
}

func TestPositionsThatAreNotSent(t *testing.T) {
	t.Parallel()
	for name, tc := range map[string]struct {
		setup func(g *gateway)
		pos   func(g *gateway) models.Position
	}{
		"aprs off": {
			setup: func(g *gateway) {
				require.NoError(t, models.SetPositionOptions(g.db, owner, owner, models.PositionOptions{Interval: 10 * time.Minute, Retention: 24 * time.Hour}))
			},
		},
		"untracked": {
			setup: func(g *gateway) {
				require.NoError(t, models.SetPositionTracking(g.db, owner, owner, false, time.Now()))
			},
		},
		"unlicensed": {
			setup: func(g *gateway) {
				require.NoError(t, g.db.Model(&models.User{}).Where("id = ?", owner).Update("callsign", "GUEST").Error)
			},
		},
		"suspended": {
			setup: func(g *gateway) {
				require.NoError(t, g.db.Model(&models.User{}).Where("id = ?", owner).Update("suspended", true).Error)
			},
		},
		"stale": {
			pos: func(g *gateway) models.Position {
				p := g.at(30.2672, -97.7431)
				p.ReceivedAt = p.ReceivedAt.Add(-3 * time.Minute)
				return p
			},
		},
		"old fix": {
			pos: func(g *gateway) models.Position {
				p := g.at(30.2672, -97.7431)
				fix := p.ReceivedAt.Add(-11 * time.Minute)
				p.FixTime = &fix
				return p
			},
		},
		"0, 0": {
			pos: func(g *gateway) models.Position { return g.at(0, 0) },
		},
		"unknown repeater": {
			pos: func(g *gateway) models.Position {
				p := g.at(30.2672, -97.7431)
				p.RepeaterID = 1
				return p
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			g := newGateway(t)
			if tc.setup != nil {
				tc.setup(g)
			}
			_, w := g.start()
			p := g.at(30.2672, -97.7431)
			if tc.pos != nil {
				p = tc.pos(g)
			}
			g.pusher.Push(p)
			assert.False(t, w.sent(t))
			assert.Empty(t, g.srv.Lines())
		})
	}
}

func TestOnlyOneReplicaLogsIn(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	_, w := g.start()
	_, w2 := g.open()
	require.False(t, next(t, w2.refreshed, "lease check"), "the second replica took the lease")
	assert.Len(t, g.srv.Logins(), 1)
	g.pusher.Push(g.at(30.2672, -97.7431))
	assert.True(t, w.sent(t), "the replica with the lease sends")
	assert.False(t, w2.sent(t), "the other replica sends nothing")
	assert.Len(t, g.lines(1), 1)
}

func TestTheReplicaThatIsLeftTakesOverTheLogin(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	first, _ := g.start()
	_, w2 := g.open()
	require.False(t, next(t, w2.refreshed, "lease check"), "the second replica took the lease")
	first.Stop()
	require.True(t, next(t, w2.logins, "login reply of the second replica"))
	assert.Len(t, g.srv.Logins(), 2)
}

func TestWithoutALoginTheGatewayConnectsNowhere(t *testing.T) {
	t.Parallel()
	g := newGateway(t)
	g.cfg.DMR.Positions.APRSIS.Callsign = ""
	_, w := g.open()
	assert.False(t, next(t, w.refreshed, "lease check"), "the gateway started an APRS-IS client")
	assert.Empty(t, g.srv.Logins())
}
