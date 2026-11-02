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
	"errors"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// TestAReadAfterAFailedReloadDoesNotTakeALoadBeforeIt changes a repeater
// while the first read of it loads it, and reloads it for the change; the
// reload fails, so the hub forgets the repeater. A read after the reload
// must get the changed repeater, not the answer of the load that began
// before the change and still runs.
func TestAReadAfterAFailedReloadDoesNotTakeALoadBeforeIt(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, database := makeTestHub(t)
		seedUser(t, database, 1000001, "TESTUSER")
		seedRepeater(t, database, 100002, 1000001)

		var reads atomic.Int32
		release := make(chan struct{})
		require.NoError(t, database.Callback().Query().After("gorm:query").Register("test:hold_first_read", func(tx *gorm.DB) {
			if !queriesRepeater(tx, 100002) {
				return
			}
			switch reads.Add(1) {
			case 1:
				<-release
			case 2:
				_ = tx.AddError(errors.New("the database failed"))
			}
		}))

		go func() { _, _ = h.RoutingRepeater(100002) }()
		synctest.Wait()
		require.NoError(t, database.Model(&models.Repeater{}).Where("id = ?", 100002).UpdateColumn("callsign", "K2CALL").Error)
		h.ReloadCachedRepeater(100002)

		got := make(chan string, 1)
		go func() {
			rpt, err := h.RoutingRepeater(100002)
			if err != nil {
				got <- err.Error()
				return
			}
			got <- rpt.Callsign
		}()
		synctest.Wait()
		close(release)
		assert.Equal(t, "K2CALL", <-got, "a read after a reload got the answer of a load that began before it")
	})
}
