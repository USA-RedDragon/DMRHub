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

package ipsc

import (
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/require"
)

// TestHandOverEveryBurstOfASlowPeerMove sends a call of 100 bursts on one
// slot to a peer of a replica that drains, and then hands the peer over
// from before the first burst, as when the move reached the replica late
// on a starved machine. The handover must hold every burst.
func TestHandOverEveryBurstOfASlowPeerMove(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		log := newSendLog()
		p := newPacer(clock.Real(), log.send, nil)
		defer p.stop()
		p.history = func() (time.Duration, int, bool) {
			keep, perSlot := hub.HandoverHistory(25 * time.Second)
			return keep, perSlot, true
		}
		k := slotKey{peer: 311860, slot: false}
		since := time.Now().Add(hub.MoveSlack)
		time.Sleep(hub.MoveSlack + time.Millisecond)
		const bursts = 100
		for i := range bursts {
			p.enqueue(k, 7, []byte{1}, i == bursts-1, models.Packet{StreamID: 7, Seq: uint(i)}, &streamSnapshot{}, time.Time{})
		}
		for range bursts {
			<-log.got
		}
		synctest.Wait()
		_, lost, _ := p.handOver(k.peer, since)
		require.Equal(t, bursts, len(lost), "the handover lost bursts that left after the move")
	})
}
