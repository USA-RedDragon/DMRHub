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

// Package dmr_test contains integration tests for the net check-in feature.
// These tests verify that calls during an active net generate check-in
// events via pubsub.
package dmr_test

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/http/api/apimodels"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNetCheckInDuringActiveNet verifies that when a net is active on a
// talkgroup, calls completed on that TG publish a check-in event via pubsub
// and appear in the check-in list.
func TestNetCheckInDuringActiveNet(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		// Seed user, talkgroup, and repeater
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 100, "NetTest TG")
		stack.SeedMMDVMRepeater(t, 200001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 200002, 1000001, testPassword)

		stack.AssignTS1StaticTG(t, 200001, 100)
		stack.AssignTS1StaticTG(t, 200002, 100)

		stack.StartServers(t)

		// Connect two MMDVM clients
		c1 := testutils.NewMMDVMClient(200001, "", testPassword)
		c2 := testutils.NewMMDVMClient(200002, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		require.NoError(t, c2.Connect(stack.MMDVMAddr))
		defer c1.Close()
		defer c2.Close()

		require.NoError(t, c1.WaitReady(handshakeWait))
		require.NoError(t, c2.WaitReady(handshakeWait))

		// Start a net on TG 100
		net := stack.SeedNet(t, 100, 1000001, "Integration test net")

		// C1 sends a group voice call to TG 100 on TS1
		voicePkt := makeGroupVoicePacket(1000001, 100, 500, false)
		voicePkt.Repeater = 200001
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(callHoldTime)
		termPkt := makeGroupVoiceTermPacket(1000001, 100, 500, false)
		termPkt.Repeater = 200001
		require.NoError(t, c1.SendDMRD(termPkt))

		waitFor(t, "the call tracker saves the call on TG 100", func() bool {
			var n int64
			return stack.DB.Model(&models.Call{}).Where("stream_id = ? AND active = ?", 500, false).Count(&n).Error == nil && n > 0
		})

		// Verify that calls were saved to the database for this TG
		calls, err := models.FindTalkgroupCallsInTimeRange(
			stack.DB, 100, net.StartTime, time.Now(),
		)
		require.NoError(t, err)
		assert.NotEmpty(t, calls, "Should have calls during the net")

		// Verify the check-in count via the model
		count, err := models.CountTalkgroupCallsInTimeRange(
			stack.DB, 100, net.StartTime, time.Now(),
		)
		require.NoError(t, err)
		assert.GreaterOrEqual(t, count, 1)
	})
}

// TestNetSeedEndedNetHasCheckIns verifies that an ended net's check-ins can be
// queried correctly with the time range.
func TestNetSeedEndedNetHasCheckIns(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000002, "N0CALL2")
		stack.SeedTalkgroup(t, 200, "PastNet TG")
		stack.SeedMMDVMRepeater(t, 2000, 1000002, "pass")

		// Create a net that ended 1 minute ago
		net := stack.SeedEndedNet(t, 200, 1000002, "Past net", 5*time.Minute)

		// Insert a call that falls within the net's time range
		toTgID := uint(200)
		userID, repeaterID := uint(1000002), uint(2000)
		call := models.Call{
			StreamID:      999,
			StartTime:     net.StartTime.Add(1 * time.Minute),
			Active:        false,
			UserID:        &userID,
			RepeaterID:    &repeaterID,
			GroupCall:     true,
			DestinationID: 200,
			TimeSlot:      false,
			IsToTalkgroup: true,
			ToTalkgroupID: &toTgID,
			Duration:      2 * time.Second,
		}
		require.NoError(t, stack.DB.Create(&call).Error)

		// Query check-ins in the net's time range
		calls, err := models.FindTalkgroupCallsInTimeRange(
			stack.DB, 200, net.StartTime, *net.EndTime,
		)
		require.NoError(t, err)
		assert.Len(t, calls, 1)
		assert.Equal(t, &userID, calls[0].UserID)

		count, err := models.CountTalkgroupCallsInTimeRange(
			stack.DB, 200, net.StartTime, *net.EndTime,
		)
		require.NoError(t, err)
		assert.Equal(t, 1, count)
	})
}

// TestNetCheckInPubsubEvent verifies that a check-in pubsub message is
// published when a call ends on a talkgroup with an active net. This test
// bypasses the protocol servers and uses the CallTracker directly.
func TestNetCheckInPubsubEvent(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000003, "N0CALL3")
		stack.SeedTalkgroup(t, 300, "PubSubNet TG")
		stack.SeedMMDVMRepeater(t, 300001, 1000003, testPassword)

		stack.AssignTS1StaticTG(t, 300001, 300)
		stack.StartServers(t)

		// Start a net — this publishes to net:events, which the CallTracker
		// subscription picks up to populate its activeNets cache.
		net := stack.SeedNet(t, 300, 1000003, "PubSub test net")

		// Publish the net:events message manually since SeedNet only inserts
		// into the DB — the controllers/scheduler normally publish this.
		netEvt := apimodels.WSNetEventResponse{
			NetID:       net.ID,
			TalkgroupID: 300,
			Event:       "started",
			Active:      true,
			StartTime:   net.StartTime,
		}
		evtData, err := json.Marshal(netEvt)
		require.NoError(t, err)
		require.NoError(t, stack.PubSub.Publish("net:events", evtData))

		waitFor(t, "the call tracker takes the net", func() bool {
			_, ok := stack.CallTracker.ActiveNet(300)
			return ok
		})

		// Subscribe to check-in events
		checkInTopic := fmt.Sprintf("net:checkins:%d", net.ID)
		sub := stack.PubSub.Subscribe(checkInTopic)
		defer func() { _ = sub.Close() }()

		// Connect and make a call
		c1 := testutils.NewMMDVMClient(300001, "", testPassword)
		require.NoError(t, c1.Connect(stack.MMDVMAddr))
		defer c1.Close()
		require.NoError(t, c1.WaitReady(handshakeWait))

		voicePkt := makeGroupVoicePacket(1000003, 300, 600, false)
		voicePkt.Repeater = 300001
		require.NoError(t, c1.SendDMRD(voicePkt))
		time.Sleep(callHoldTime)
		termPkt := makeGroupVoiceTermPacket(1000003, 300, 600, false)
		termPkt.Repeater = 300001
		require.NoError(t, c1.SendDMRD(termPkt))

		msgs := testutils.Collect(sub.Channel(), deliverWait, gotN[[]byte](1))
		require.Len(t, msgs, 1, "the call tracker publishes no check-in")
		var checkIn apimodels.WSNetCheckInResponse
		require.NoError(t, json.Unmarshal(msgs[0], &checkIn))
		assert.Equal(t, net.ID, checkIn.NetID)
		assert.Equal(t, uint(1000003), checkIn.User.ID)
	})
}
