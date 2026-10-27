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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/stretchr/testify/assert"
)

func waitForCalls(h interface{ WaitForCalls() }) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		h.WaitForCalls()
		close(done)
	}()
	return done
}

func TestWaitForCallsWaitsForDataStreams(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, 1000001, "CALLER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	pkt := dataHeader(1000001, 1, 100001, 4242)
	routed := time.Now()
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	settle(t, h, pkt, models.RepeaterTypeMMDVM)
	done := waitForCalls(h)
	select {
	case <-done:
		assert.GreaterOrEqual(t, time.Since(routed), hub.StreamIdle, "shutdown did not wait for a data call in progress")
	case <-time.After(failAfter):
		t.Fatal("shutdown waited for a data call that had ended")
	}
}

func TestWaitForCallsEndsVoiceWithoutTerminator(t *testing.T) {
	t.Parallel()
	h, database := makeTestHub(t)
	seedUser(t, database, 1000001, "CALLER")
	seedRepeater(t, database, 100001, 1000001)
	seedTalkgroup(t, database, 1, "TG1")

	pkt := makeVoicePacket(1, 4343, true, false)
	h.RoutePacket(context.Background(), pkt, models.RepeaterTypeMMDVM)
	settle(t, h, pkt, models.RepeaterTypeMMDVM)
	select {
	case <-waitForCalls(h):
	case <-time.After(failAfter):
		t.Fatal("shutdown waited for ever on a voice call that lost its terminator")
	}
}
