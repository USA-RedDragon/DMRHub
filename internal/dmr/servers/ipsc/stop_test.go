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
	"context"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/servers"
	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

func TestStopDeregistersPeers(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	register(t, s, client, addr, 77)

	if err := s.Stop(context.Background()); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	r := readUDP(t, client)
	if ipscwire.Opcode(r[0]) != ipscwire.OpDeregisterRequest {
		t.Fatalf("got opcode 0x%02x at Stop, want a deregistration request", r[0])
	}
	if len(s.peers) != 0 {
		t.Fatalf("%d peers left after Stop", len(s.peers))
	}
}

func TestStopInAHandoffSendsNothing(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	register(t, s, client, addr, 77)

	if err := s.Stop(servers.WithGracefulHandoff(context.Background(), true)); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	expectNoUDP(t, client)
}
