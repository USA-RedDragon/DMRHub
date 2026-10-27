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
	"github.com/stretchr/testify/require"
)

// failAfter is how long a test waits for the hub before it fails. No test
// passes on a time bound: each waits for the event it needs, and failAfter
// only ends a test that would hang.
const failAfter = 30 * time.Second

// knowTalkgroups waits until the hub knows whether each talkgroup in the
// database exists, so that the first burst to one does not wait on the
// database. A slow database makes the hub drop that burst instead.
func knowTalkgroups(t testing.TB, h *hub.Hub) {
	t.Helper()
	require.NoError(t, h.KnowTalkgroups())
}

// settle waits until the hub has routed the bursts that came before from
// the source slot of like from sourceName, made the talkgroup link changes
// that they asked for, and published them.
func settle(t testing.TB, h *hub.Hub, like models.Packet, sourceName string) {
	t.Helper()
	select {
	case <-h.Settled(like, sourceName):
	case <-time.After(failAfter):
		t.Fatalf("the hub did not settle the source slot in %s", failAfter)
	}
}

// receive returns the next packet that srv gets.
func receive(t testing.TB, srv *hub.ServerHandle) hub.RoutedPacket {
	t.Helper()
	select {
	case rp := <-srv.Packets:
		return rp
	case <-time.After(failAfter):
		t.Fatalf("no packet in %s", failAfter)
		return hub.RoutedPacket{}
	}
}

// flush routes a private burst from the source slot of like on the MMDVM
// server to the repeater to, and returns the packets that srv got before
// it. The hub hands a repeater the bursts of a source slot in the order
// they came, so a burst routed before that the repeater did not get by
// then, it never gets.
func flush(t testing.TB, h *hub.Hub, srv *hub.ServerHandle, like models.Packet, to uint) []hub.RoutedPacket {
	t.Helper()
	require.NoError(t, h.KnowRepeater(to))
	sentinel := like
	sentinel.Dst = to
	sentinel.GroupCall = false
	sentinel.StreamID = like.StreamID | 0x40000000
	h.RoutePacket(context.Background(), sentinel, models.RepeaterTypeMMDVM)
	var before []hub.RoutedPacket
	for {
		rp := receive(t, srv)
		if rp.RepeaterID == to && rp.Packet.StreamID == sentinel.StreamID {
			return before
		}
		before = append(before, rp)
	}
}
