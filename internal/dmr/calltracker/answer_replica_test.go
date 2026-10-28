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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/testutils/bubble"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func radioCheckPair(t *testing.T) (pdu.CSBK, pdu.CSBK) {
	t.Helper()
	var req, ans pdu.CSBK
	require.NoError(t, req.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	require.NoError(t, ans.SetMotorolaRadioCheck(pdu.MotorolaRadioCheckPDU{Answer: true, RequesterAddress: uint32(userA), CheckedAddress: uint32(userB)}))
	return req, ans
}

func radioCheckRows(t *testing.T, heardFirst bool) map[uint]models.Call {
	t.Helper()
	req, ans := radioCheckPair(t)
	return answeredRows(t, req, ans, heardFirst, 0)
}

// answeredRows sends req from userA to userB to one tracker and ans from
// userB to userA to another that shares its database, pubsub and KV, ans
// first when heardFirst is set, gap after req otherwise. It returns the
// rows by stream: 7062 for req and 7063 for ans.
func answeredRows(t *testing.T, req, ans pdu.CSBK, heardFirst bool, gap time.Duration) map[uint]models.Call {
	t.Helper()
	one, two, database := newReplicas(t)
	req.LastBlock, ans.LastBlock = true, true
	if heardFirst {
		newSender(t, two, userB, userA, false, 7063).data(burstOf(t, &ans))
		two.Flush()
		newSender(t, one, userA, userB, false, 7062).data(burstOf(t, &req))
	} else {
		newSender(t, one, userA, userB, false, 7062).data(burstOf(t, &req))
		time.Sleep(gap)
		newSender(t, two, userB, userA, false, 7063).data(burstOf(t, &ans))
	}
	byStream := map[uint]models.Call{}
	bubble.Eventually(t, func() bool {
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

func TestTrackCallAlertAnsweredOnAnotherReplica(t *testing.T) {
	t.Parallel()
	for _, heardFirst := range []bool{false, true} {
		t.Run(map[bool]string{false: "request first", true: "answer first"}[heardFirst], func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				var req, ans pdu.CSBK
				require.NoError(t, req.SetMotorolaCallAlert(pdu.MotorolaCallAlertPDU{TargetAddress: uint32(userB), SourceAddress: uint32(userA)}))
				require.NoError(t, ans.SetMotorolaCallAlertAck(pdu.MotorolaCallAlertAckPDU{Request: 0x9F, TargetAddress: uint32(userA), SourceAddress: uint32(userB)}))
				byStream := answeredRows(t, req, ans, heardFirst, 0)
				assert.Equal(t, "call_alert", byStream[7062].SubKind)
				assert.Equal(t, models.CallResponseACK, byStream[7062].Response, "the call alert is answered")
				_, own := byStream[7063]
				assert.False(t, own, "the answer makes no row of its own")
			})
		})
	}
}

// TestTrackLateAnswerOnAnotherReplica answers a radio check on another
// replica after the response window. The answer must not be set on the
// request and keeps a row of its own.
func TestTrackLateAnswerOnAnotherReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		req, ans := radioCheckPair(t)
		byStream := answeredRows(t, req, ans, false, 10*time.Second+500*time.Millisecond)
		assert.Equal(t, models.CallResponseNone, byStream[7062].Response, "a late answer is not set on the request")
		if assert.Contains(t, byStream, uint(7063), "the late answer keeps its own row") {
			assert.Equal(t, "radio_check_answer", byStream[7063].SubKind)
		}
	})
}

func TestTrackAnswerOnAnotherReplicaAtOnce(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		byStream := radioCheckRows(t, false)
		assert.Equal(t, models.CallResponseACK, byStream[7062].Response, "the radio check is answered")
		_, own := byStream[7063]
		assert.False(t, own, "the answer makes no row of its own")
	})
}

func TestTrackAnswerHeardFirstOnAnotherReplica(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		byStream := radioCheckRows(t, true)
		assert.Equal(t, models.CallResponseACK, byStream[7062].Response, "the radio check is answered")
		_, own := byStream[7063]
		assert.False(t, own, "the answer makes no row of its own")
	})
}
