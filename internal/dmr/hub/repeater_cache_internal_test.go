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

package hub

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
)

// TestARefusedReloadLeavesNoEntryFromBeforeIt overlaps two reloads of a
// repeater, as an explicit reload and the reload of a change notice do:
// the first stores after the second began, so its store is refused. The
// entry from before both must not stay readable then.
func TestARefusedReloadLeavesNoEntryFromBeforeIt(t *testing.T) {
	t.Parallel()
	c := newRepeaterCache(clock.Real())
	tg := uint(11)
	now := time.Now()
	c.store(100001, &models.Repeater{TS2DynamicTalkgroupID: &tg}, c.generation(), now)

	first := c.bump()
	second := c.bump()
	c.store(100001, &models.Repeater{}, first, now)
	e, ok := c.entries.Load(100001)
	assert.False(t, ok && e.rpt != nil && e.rpt.TS2DynamicTalkgroupID != nil, "the entry from before the reloads stays readable after a refused store")

	c.store(100001, &models.Repeater{}, second, now)
	e, ok = c.entries.Load(100001)
	assert.True(t, ok && e.rpt != nil && e.rpt.TS2DynamicTalkgroupID == nil, "the last reload stores its load")
}
