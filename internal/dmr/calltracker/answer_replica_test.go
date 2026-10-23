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
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func radioCheckRows(t *testing.T, heardFirst bool) map[uint]models.Call {
	t.Helper()
	one, two, database := newReplicas(t)
	var req, ans pdu.CSBK
	req.LastBlock, ans.LastBlock = true, true
	require.NoError(t, req.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	require.NoError(t, ans.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{Answer: true, RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	if heardFirst {
		newSender(t, two, userB, userA, false, 7063).data(burstOf(t, &ans))
		two.Flush()
		newSender(t, one, userA, userB, false, 7062).data(burstOf(t, &req))
	} else {
		newSender(t, one, userA, userB, false, 7062).data(burstOf(t, &req))
		newSender(t, two, userB, userA, false, 7063).data(burstOf(t, &ans))
	}
	byStream := map[uint]models.Call{}
	require.Eventually(t, func() bool {
		one.Flush()
		two.Flush()
		clear(byStream)
		for _, c := range rows(t, database) {
			if c.Active {
				return false
			}
			byStream[c.StreamID] = c
		}
		return len(byStream) > 0
	}, 8*time.Second, 50*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	one.Flush()
	two.Flush()
	clear(byStream)
	for _, c := range rows(t, database) {
		byStream[c.StreamID] = c
	}
	return byStream
}

func TestTrackAnswerOnAnotherReplicaAtOnce(t *testing.T) {
	t.Parallel()
	byStream := radioCheckRows(t, false)
	assert.Equal(t, models.CallResponseACK, byStream[7062].Response, "the radio check is answered")
	_, own := byStream[7063]
	assert.False(t, own, "the answer makes no row of its own")
}

func TestTrackAnswerHeardFirstOnAnotherReplica(t *testing.T) {
	t.Parallel()
	byStream := radioCheckRows(t, true)
	assert.Equal(t, models.CallResponseACK, byStream[7062].Response, "the radio check is answered")
	_, own := byStream[7063]
	assert.False(t, own, "the answer makes no row of its own")
}
