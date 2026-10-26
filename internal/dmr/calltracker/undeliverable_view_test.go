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

package calltracker_test

import (
	"encoding/json"
	"testing"
	"time"

	dmrconst "github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestPublicCallShowsNoUndeliverable marks a group call undeliverable to a
// repeater and reads the rows that the public websocket sends. A visitor
// who is not signed in sees none of the repeaters it was not sent to.
func TestPublicCallShowsNoUndeliverable(t *testing.T) {
	t.Parallel()
	ct, database, ps := newTracker(t)
	sub := ps.Subscribe("calls:public")
	t.Cleanup(func() { _ = sub.Close() })

	s := newSender(t, ct, userA, tgID, true, 7201)
	p := s.send(dmrconst.FrameDataSync, uint(enums.DataTypeIdle), burstOf(t, &pdu.PRFill{}))
	ct.Undeliverable(p, "IPSC repeater 314926 was busy on that slot.")
	c := waitDone(t, ct, database, 1)[0]
	require.Len(t, c.Undeliverable, 1, "the row keeps the repeater")

	seen := 0
	for {
		select {
		case raw := <-sub.Channel():
			var row map[string]any
			require.NoError(t, json.Unmarshal(raw, &row))
			seen++
			assert.Empty(t, row["undeliverable"], "a visitor sees where the call was not sent: %s", raw)
			assert.Empty(t, row["undeliverable_scope"], "a visitor sees where the call was not sent: %s", raw)
		case <-time.After(2 * time.Second):
			require.NotZero(t, seen, "no public row")
			return
		}
	}
}
