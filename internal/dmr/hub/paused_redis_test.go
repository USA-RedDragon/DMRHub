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
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const pausedKVCall = time.Second

// pausedKV acts as a Redis that stops while paused is set, for the keys
// that start with prefix: each call waits a second and fails.
type pausedKV struct {
	kv.KV
	prefix string
	paused atomic.Bool
	calls  atomic.Int64
}

var errPausedKV = errors.New("read tcp: i/o timeout")

func (p *pausedKV) stalls(key string) bool {
	if !p.paused.Load() || !strings.HasPrefix(key, p.prefix) {
		return false
	}
	p.calls.Add(1)
	time.Sleep(pausedKVCall)
	return true
}

func (p *pausedKV) Get(ctx context.Context, key string) ([]byte, error) {
	if p.stalls(key) {
		return nil, errPausedKV
	}
	return p.KV.Get(ctx, key) //nolint:wrapcheck
}

func (p *pausedKV) Set(ctx context.Context, key string, value []byte) error {
	if p.stalls(key) {
		return errPausedKV
	}
	return p.KV.Set(ctx, key, value) //nolint:wrapcheck
}

func (p *pausedKV) Expire(ctx context.Context, key string, ttl time.Duration) error {
	if p.stalls(key) {
		return errPausedKV
	}
	return p.KV.Expire(ctx, key, ttl) //nolint:wrapcheck
}

// TestAPausedRedisLosesNoSighting hears 300 radios on a trusted repeater
// in three seconds while every call on the records of where radios were
// heard waits a second and fails, as a paused Redis makes it, for four
// seconds. The
// worker that takes the sightings must not wait on Redis, or its queue
// fills and it drops sightings, and each record must be written once
// Redis goes on.
func TestAPausedRedisLosesNoSighting(t *testing.T) {
	t.Parallel()
	paused := &pausedKV{prefix: "dmrhub:lastseen:"}
	h, database, _ := makeTestHubWrappingKV(t, func(ps pubsub.PubSub) pubsub.PubSub { return ps }, func(k kv.KV) kv.KV {
		paused.KV = k
		return paused
	})
	const radios, first = 300, 1100000
	seedUser(t, database, 1000001, "OWNER")
	seedRepeater(t, database, 100003, 1000001)
	require.NoError(t, database.Exec("UPDATE repeaters SET trusted = ? WHERE id = ?", true, 100003).Error)
	for i := range radios {
		seedUser(t, database, first+uint(i), "R"+strconv.Itoa(i))
	}
	h.RegisterServer(hub.ServerConfig{Name: models.RepeaterTypeMMDVM, Role: hub.RoleRepeater})
	h.ActivateRepeater(context.Background(), 100003)
	h.LiftSlotRate(true)
	time.Sleep(100 * time.Millisecond)

	paused.paused.Store(true)
	time.AfterFunc(4*time.Second, func() { paused.paused.Store(false) })
	for i := range radios {
		h.RoutePacket(context.Background(), dataHeader(first+uint(i), 1, 100003, 9000+uint(i)), models.RepeaterTypeMMDVM)
		time.Sleep(10 * time.Millisecond)
	}
	missing := func() []uint {
		var out []uint
		for i := range radios {
			if h.HeardAt(context.Background(), first+uint(i)) != 100003 {
				out = append(out, first+uint(i))
			}
		}
		return out
	}
	assert.Eventually(t, func() bool { return len(missing()) == 0 }, 15*time.Second, 100*time.Millisecond, "where some radios were heard is not stored")
	t.Logf("%d of %d records missing after %d paused KV calls", len(missing()), radios, paused.calls.Load())
}
