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
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errRefused = errors.New("dial tcp: connection refused")

func TestARedisThatAnswersLateIsConnected(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		tries := 0
		start := time.Now()
		err := kv.PingUntil(t.Context(), func(context.Context) error {
			tries++
			if time.Since(start) < 5*time.Second {
				return errRefused
			}
			return nil
		})
		require.NoError(t, err)
		assert.Greater(t, tries, 1, "the first pings failed")
		assert.Less(t, time.Since(start), consts.RedisConnectWait, "connected before the startup deadline")
	})
}

func TestARedisThatNeverAnswersFailsAtTheStartupDeadline(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		err := kv.PingUntil(t.Context(), func(context.Context) error { return errRefused })
		require.ErrorIs(t, err, errRefused, "the last error of the ping")
		took := time.Since(start)
		assert.GreaterOrEqual(t, took, consts.RedisConnectWait, "gave up before the startup deadline")
		assert.Less(t, took, consts.RedisConnectWait+5*time.Second, "kept trying long after the startup deadline")
	})
}

func TestTheStartupPingEndsWithItsContext(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		start := time.Now()
		err := kv.PingUntil(ctx, func(context.Context) error { return errRefused })
		require.Error(t, err)
		assert.Less(t, time.Since(start), 4*time.Second, "kept trying after the context ended")
	})
}
