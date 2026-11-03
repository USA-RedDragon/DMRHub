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

package kv_test

import (
	"context"
	"os/exec"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/configulator/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRedisCallsAreBoundedWhenRedisHangs pauses the Redis container, so
// that it takes connections and never answers. A call must return by the
// deadline of its context, and a call without one within the call timeout,
// even when the pool has enough ready connections for every retry.
func TestRedisCallsAreBoundedWhenRedisHangs(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	var c testutils.Containers
	testutils.PostgresOwnRedisBackendInto(&c).Setup(t, &cfg)
	store, err := kv.MakeKV(t.Context(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Set(t.Context(), "hang", []byte{1}))
	require.NoError(t, kv.FillRedisPool(t.Context(), store))

	docker := func(args ...string) {
		out, err := exec.Command("docker", args...).CombinedOutput() //nolint:gosec,noctx
		require.NoError(t, err, string(out))
	}
	docker("pause", c.Redis)
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", c.Redis).Run() }) //nolint:gosec,noctx

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err = store.Get(ctx, "hang")
	withDeadline := time.Since(start)
	require.Error(t, err)

	start = time.Now()
	_, err = store.Get(context.Background(), "hang")
	withoutDeadline := time.Since(start)
	require.Error(t, err)

	t.Logf("a call to a hung Redis returned after %v with a 100 ms deadline and after %v without one", withDeadline, withoutDeadline)
	assert.Less(t, withDeadline, consts.RedisCallTimeout, "the call ignored the deadline of its context and ran for the call timeout")
	assert.Less(t, withoutDeadline, 2*consts.RedisCallTimeout, "the call without a deadline was not bounded by the call timeout, so each retry got its own read timeout")
}

// socketContext is a context that reports each call of its Deadline
// method. go-redis calls it when it sets the deadline of a socket read or
// write, after the call has taken a pooled connection.
type socketContext struct {
	context.Context //nolint:containedctx // a context that wraps one
	reached         chan struct{}
}

func (c socketContext) Deadline() (time.Time, bool) {
	select {
	case c.reached <- struct{}{}:
	default:
	}
	return c.Context.Deadline()
}

// pastDeadline is a context whose deadline has passed and that has not
// ended, as a context is in the instant after its deadline.
type pastDeadline struct {
	context.Context //nolint:containedctx // a context that wraps one
}

func (pastDeadline) Deadline() (time.Time, bool) { return time.Unix(1, 0), true }

// TestAStoreWithoutACallBoundEndsOnItsCallersContext calls a hung Redis
// through a store made with WithoutCallBound, which sets no deadline of its
// own. The context of the call has no deadline and ends only after the call
// has reached the socket, where go-redis watches only deadlines. The call
// must still end with its context. A call whose deadline has passed must
// end too.
func TestAStoreWithoutACallBoundEndsOnItsCallersContext(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	var c testutils.Containers
	testutils.PostgresOwnRedisBackendInto(&c).Setup(t, &cfg)
	store, err := kv.MakeKV(t.Context(), &cfg, kv.WithoutCallBound())
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	require.NoError(t, store.Set(t.Context(), "hang", []byte{1}))
	require.NoError(t, kv.FillRedisPool(t.Context(), store))

	out, err := exec.Command("docker", "pause", c.Redis).CombinedOutput() //nolint:gosec,noctx
	require.NoError(t, err, string(out))
	t.Cleanup(func() { _ = exec.Command("docker", "unpause", c.Redis).Run() }) //nolint:gosec,noctx

	ctx, cancel := context.WithCancel(t.Context())
	sctx := socketContext{Context: ctx, reached: make(chan struct{}, 1)}
	got := make(chan error, 1)
	go func() {
		_, err := store.Get(sctx, "hang")
		got <- err
	}()
	<-sctx.reached
	cancel()
	require.ErrorIs(t, <-got, context.Canceled, "a call to a hung Redis without a call bound ignored the end of its context")

	_, err = store.Get(pastDeadline{t.Context()}, "hang")
	require.Error(t, err, "a call to a hung Redis without a call bound ignored the deadline of its context")
}

func TestRedisGetMissingIsNotFound(t *testing.T) {
	t.Parallel()
	cfg, err := configulator.New(config.ConfigSchema()).Default()
	require.NoError(t, err)
	var c testutils.Containers
	testutils.PostgresRedisBackendInto(&c).Setup(t, &cfg)
	store, err := kv.MakeKV(t.Context(), &cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	_, err = store.Get(t.Context(), "missing")
	assert.ErrorIs(t, err, kv.ErrNotFound)
}
