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
	"errors"
	"testing"

	ipscwire "github.com/USA-RedDragon/dmrgo/v2/transport/ipsc"
)

func TestAPacketThatReadTheKeyBeforeARekeyDoesNotRegisterThePeerAgain(t *testing.T) {
	t.Parallel()
	s, srv := newTestServerWithUDP(t)
	client, addr := dialServer(t, srv)
	register(t, s, client, addr, 9)
	old, rotated := testKey(t), [20]byte{19: 0x42}
	reads := 0
	s.keyFor = func(uint32) ([20]byte, bool, error) {
		reads++
		if reads == 1 {
			s.rekey(9)
			return old, true, nil
		}
		return rotated, true, nil
	}
	err := s.handlePacket(context.Background(), signTestPacket(t, control(t, ipscwire.OpAliveRequest, 9)), addr)
	if !errors.Is(err, ErrPacketIgnored) {
		t.Fatalf("a keepalive signed with the old key after a rekey: err %v, want ErrPacketIgnored", err)
	}
	if peers(s, 9) != 0 {
		t.Fatal("a keepalive signed with the old key registered the peer again after the rekey dropped it")
	}
	expectNoUDP(t, client)
}
