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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestActivatingARepeaterReadsItOnce activates a repeater as its login
// does. The hub must read the repeater once for its cache and its
// subscriptions, since a login waits on it, and both must follow that
// read.
func TestActivatingARepeaterReadsItOnce(t *testing.T) {
	t.Parallel()
	h, database, _ := makeTestHubWrappingKV(t, keepPubSub, keepKV, clock.NewFake(time.Now()))
	seedUser(t, database, 1000001, "N0CALL")
	seedTalkgroup(t, database, 7, "TG7")
	seedRepeater(t, database, 100001, 1000001)
	require.NoError(t, database.Exec("INSERT INTO repeater_ts1_static_talkgroups (repeater_id, talkgroup_id) VALUES (?, ?)", 100001, 7).Error)

	reads := countTableQueries(t, database, "repeaters")
	h.ActivateRepeaterSince(context.Background(), 100001, time.Now())
	assert.EqualValues(t, 1, reads.Load(), "the activation of a repeater read it more than once")
	assert.True(t, h.Subscribes(100001, 7), "the repeater did not subscribe to its static talkgroup")
}
