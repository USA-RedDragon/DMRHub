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

package calltracker

import (
	"context"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// ResponseWindow is how long before an answer the row it answers can
// start, and how long the answer stays pending.
const ResponseWindow = responseWindow

// HoldWorker holds the worker of the slot of repeater on server until the
// returned func runs.
func HoldWorker(c *CallTracker, server string, repeater uint, slot bool) func() {
	gate := make(chan struct{})
	c.enqueue(WorkerOf(server, repeater, slot), task{fn: func(*shard) { <-gate }})
	return sync.OnceFunc(func() { close(gate) })
}

// WorkerOf returns the worker of the slot of repeater on server.
func WorkerOf(server string, repeater uint, slot bool) int {
	return chanKey{server: server, repeater: repeater, slot: slot}.shard()
}

const VoiceHang = voiceHang

const SaveTimeout = saveTimeout

func SaveMergedPart(c *CallTracker, call *models.Call, id string, packets uint) bool {
	_, _, ok, _, _ := c.shards[0].saveMerged(context.Background(), call, []voiceDelta{{ID: id, Packets: packets, First: call.StartTime, Last: call.StartTime}})
	return ok
}

// WorkerAnswers returns a channel that closes when the worker of the slot
// of repeater on server runs a task queued now.
func WorkerAnswers(c *CallTracker, server string, repeater uint, slot bool) <-chan struct{} {
	done := make(chan struct{})
	c.enqueue(WorkerOf(server, repeater, slot), task{fn: func(*shard) { close(done) }})
	return done
}
