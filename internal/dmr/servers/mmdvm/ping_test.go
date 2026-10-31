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
package mmdvm

import (
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestPingWritesOnlyTheLastPingTime(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311860)
	r.login(dmrd.DialectMMDVM, 311860)
	r.s.pingWG.Wait()

	var statements atomic.Int64
	count := func(*gorm.DB) { statements.Add(1) }
	cb := r.s.DB.Callback()
	require.NoError(t, cb.Query().After("gorm:query").Register("test:count_query", count))
	require.NoError(t, cb.Row().After("gorm:row").Register("test:count_row", count))
	require.NoError(t, cb.Raw().After("gorm:raw").Register("test:count_raw", count))
	require.NoError(t, cb.Create().After("gorm:create").Register("test:count_create", count))
	require.NoError(t, cb.Update().After("gorm:update").Register("test:count_update", count))

	before := time.Now()
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTPING, ID: 311860})
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTPONG)
	r.s.pingWG.Wait()
	assert.Equal(t, int64(1), statements.Load(), "statements for one ping")
	var row models.Repeater
	require.NoError(t, r.s.DB.First(&row, 311860).Error)
	assert.False(t, row.LastPing.Before(before.Truncate(time.Second)), "last ping %v, want after %v", row.LastPing, before)
}

func TestASlowPingWriteDoesNotHoldUpThePackets(t *testing.T) {
	t.Parallel()
	r := newDialectRig(t, 311860)
	r.login(dmrd.DialectMMDVM, 311860)
	r.s.pingWG.Wait()
	var inHandler, stores atomic.Int32
	require.NoError(t, r.s.DB.Callback().Update().Before("gorm:update").Register("test:store_stack", func(*gorm.DB) {
		stores.Add(1)
		if strings.Contains(string(debug.Stack()), "(*Server).handle") {
			inHandler.Add(1)
		}
	}))

	before := time.Now()
	r.control(dmrd.DialectMMDVM, dmrd.Control{Command: dmrd.CommandRPTPING, ID: 311860})
	r.expect(dmrd.DialectMMDVM, dmrd.CommandMSTPONG)
	r.s.pingWG.Wait()

	require.NotZero(t, stores.Load(), "the last ping is written")
	assert.Zero(t, inHandler.Load(), "the packet handler stored the last ping itself, so a slow store holds up the packets")
	var row models.Repeater
	require.NoError(t, r.s.DB.First(&row, 311860).Error)
	assert.False(t, row.LastPing.Before(before.Truncate(time.Second)), "last ping %v, want after %v", row.LastPing, before)
}
