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
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/assert"
)

// TestReleaseIngressWaitsForTheCopies reserves the place of the copy in
// the other codec of a burst of a repeater, as the hub does while it
// converts the burst. The drain of the ingress of the repeater must wait
// until that copy was routed, so that the copies of a moved talker keep
// their order too.
func TestReleaseIngressWaitsForTheCopies(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })
		key := sourceSlot{streamSource: streamSource{server: models.RepeaterTypeMMDVM, repeater: 100001}}
		ticket := h.reserveCopy(key, models.RepeaterTypeMMDVM)
		start := time.Now()
		returned := make(chan bool, 1)
		go func() { returned <- h.waitCopies(key, nil) }()
		time.Sleep(2 * time.Second)
		synctest.Wait()
		select {
		case <-returned:
			t.Fatal("the drain did not wait for the copy on its way")
		default:
		}
		h.finishCopy(context.Background(), ticket)
		assert.True(t, <-returned)
		assert.Equal(t, 2*time.Second, time.Since(start))
		h.copyMu.Lock()
		defer h.copyMu.Unlock()
		assert.Empty(t, h.copyOrders, "the order of the copies outlived them")
	})
}
