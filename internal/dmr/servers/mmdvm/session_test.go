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

package mmdvm

import (
	"errors"
	"net"
	"testing"
)

func TestPendingSessionsAreCapped(t *testing.T) {
	t.Parallel()
	var s Server
	for i := range maxPending {
		_, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, byte(i>>8), byte(i)), Port: 62031})
		if err != nil {
			t.Fatalf("link %d: %v", i, err)
		}
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 1, 0, 0), Port: 62031}); !errors.Is(err, errTooManyPending) {
		t.Fatalf("got %v, want errTooManyPending", err)
	}
	if _, err := s.linkFor(net.UDPAddr{IP: net.IPv4(10, 0, 0, 0), Port: 62031}); err != nil {
		t.Fatalf("known address: %v", err)
	}
}
