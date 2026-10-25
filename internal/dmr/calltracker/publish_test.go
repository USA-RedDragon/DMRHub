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
	"github.com/USA-RedDragon/DMRHub/internal/dmr/calltracker"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const slowPublish = 500 * time.Millisecond

type slowPubSub struct{ pubsub.PubSub }

func (p slowPubSub) Publish(topic string, message []byte) error {
	time.Sleep(slowPublish)
	return p.PubSub.Publish(topic, message) //nolint:wrapcheck
}

// TestMarksForOtherReplicasWaitOnNoPublish marks a burst of a call that
// another replica tracks as undeliverable and as converted while every
// publish takes 500 ms. The callers deliver bursts, some under a lock of
// their server, so they must not wait for the publish, and each mark must
// still reach the other replicas.
func TestMarksForOtherReplicasWaitOnNoPublish(t *testing.T) {
	t.Parallel()
	database, ps, kvStore := trackerDeps(t)
	ct := calltracker.NewCallTracker(database, slowPubSub{ps}, kvStore)
	t.Cleanup(ct.Stop)
	undeliverable := ps.Subscribe("calltracker:undeliverable")
	t.Cleanup(func() { _ = undeliverable.Close() })
	converted := ps.Subscribe("calltracker:converted")
	t.Cleanup(func() { _ = converted.Close() })

	p := models.Packet{StreamID: 777, Src: userA, Dst: tgID, GroupCall: true}
	start := time.Now()
	ct.Undeliverable(p, "ipsc:311304200:slot_busy")
	ct.Converted(p, "codec2", "repeater:311860")
	waited := time.Since(start)
	t.Logf("marking two bursts waited %v with %v per publish", waited, slowPublish)
	assert.Less(t, waited, 20*time.Millisecond, "a mark waited on pubsub")

	for name, sub := range map[string]pubsub.Subscription{"undeliverable": undeliverable, "converted": converted} {
		select {
		case <-sub.Channel():
		case <-time.After(5 * time.Second):
			require.Fail(t, "the mark did not reach the other replicas", name)
		}
	}
}
