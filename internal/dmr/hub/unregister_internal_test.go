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

package hub

import (
	"context"
	"testing"
	"testing/synctest"
	"time"
)

// TestUnregisterServerWithAFullChannel stops a server that no longer reads
// its channel while a delivery waits on the full channel, as when a server
// stops under traffic. UnregisterServer must not wait for that delivery.
func TestUnregisterServerWithAFullChannel(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := NewHub(nil, nil, nil, nil)
		t.Cleanup(func() { h.Stop(context.Background()) })

		h.RegisterServer(ServerConfig{Name: "stalled", Role: RoleRepeater})
		for range serverChannelSize {
			h.deliverToServer("stalled", RoutedPacket{RepeaterID: 1})
		}
		delivered := make(chan struct{})
		go func() {
			defer close(delivered)
			h.deliverToServer("stalled", RoutedPacket{RepeaterID: 1})
		}()
		time.Sleep(50 * time.Millisecond)

		unregistered := make(chan struct{})
		go func() {
			defer close(unregistered)
			h.UnregisterServer("stalled")
		}()
		select {
		case <-unregistered:
		case <-time.After(2 * time.Second):
			t.Fatal("UnregisterServer waits for a delivery to a channel that nobody reads")
		}
		select {
		case <-delivered:
		case <-time.After(2 * time.Second):
			t.Fatal("the delivery to the unregistered server still waits")
		}
	})
}
