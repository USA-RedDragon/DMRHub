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

package redisping_test

import (
	"context"
	"errors"
	"net"
	"os"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/consts"
	"github.com/USA-RedDragon/DMRHub/internal/redisping"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func refused() error {
	return &net.OpError{Op: "dial", Net: "tcp", Err: os.NewSyscallError("connect", syscall.ECONNREFUSED)}
}

func TestARedisThatAnswersLateIsConnected(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		tries := 0
		start := time.Now()
		err := redisping.Until(t.Context(), func(context.Context) error {
			tries++
			if time.Since(start) < 5*time.Second {
				return refused()
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
		err := redisping.Until(t.Context(), func(context.Context) error { return refused() })
		require.ErrorIs(t, err, syscall.ECONNREFUSED, "the last error of the ping")
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
		err := redisping.Until(ctx, func(context.Context) error { return refused() })
		require.Error(t, err)
		assert.Less(t, time.Since(start), 4*time.Second, "kept trying after the context ended")
	})
}

func TestAPermanentErrorFailsAtOnce(t *testing.T) {
	t.Parallel()
	for name, permanent := range map[string]error{
		"wrong password": errors.New("WRONGPASS invalid username-password pair or user is disabled."),
		"no auth":        errors.New("NOAUTH Authentication required."),
		"unknown host":   &net.OpError{Op: "dial", Net: "tcp", Err: &net.DNSError{Err: "no such host", Name: "redis.invalid", IsNotFound: true}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				tries := 0
				start := time.Now()
				err := redisping.Until(t.Context(), func(context.Context) error {
					tries++
					return permanent
				})
				require.ErrorIs(t, err, permanent)
				assert.Equal(t, 1, tries, "a permanent error was tried again")
				assert.Zero(t, time.Since(start), "a permanent error waited")
			})
		})
	}
}

func TestTransientErrorsAreTriedAgain(t *testing.T) {
	t.Parallel()
	for name, transient := range map[string]error{
		"refused":  refused(),
		"timeout":  &net.OpError{Op: "read", Net: "tcp", Err: os.ErrDeadlineExceeded},
		"deadline": context.DeadlineExceeded,
		"loading":  errors.New("LOADING Redis is loading the dataset in memory"),
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				tries := 0
				err := redisping.Until(t.Context(), func(context.Context) error {
					tries++
					if tries < 3 {
						return transient
					}
					return nil
				})
				require.NoError(t, err)
				assert.Equal(t, 3, tries)
			})
		})
	}
}
