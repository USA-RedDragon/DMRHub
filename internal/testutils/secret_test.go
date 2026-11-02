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

package testutils

import (
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/stretchr/testify/require"
)

func TestAnOpenBridgeClientTakesNothingSignedWithTheSecretOfAnotherTest(t *testing.T) {
	t.Parallel()
	other := Secret(t)
	t.Run("own", func(t *testing.T) {
		t.Parallel()
		own := Secret(t)
		require.Equal(t, own, Secret(t), "the secret of a test must not change")
		require.NotEqual(t, own, other, "two tests must not share a secret")

		receiver := NewOpenBridgeClient(500001, own)
		require.NoError(t, receiver.Connect(listenLoopback(t).LocalAddr().String()))
		t.Cleanup(receiver.Close)
		send := func(password string, stream uint) {
			c := NewOpenBridgeClient(500002, password)
			require.NoError(t, c.Connect(receiver.LocalAddr().String()))
			t.Cleanup(c.Close)
			require.NoError(t, c.SendRawPacket(models.Packet{Signature: "DMRD", Src: 1000001, Dst: 1, GroupCall: true, StreamID: stream}))
		}
		send(other, 1)
		send(own, 2)

		got := Collect(receiver.ReceivedPackets(), replyWait, func(p []models.Packet) bool { return p[len(p)-1].StreamID == 2 })
		require.NotEmpty(t, got)
		for _, p := range got {
			require.Equal(t, uint(2), p.StreamID, "the client took a packet signed with the secret of another test")
		}
	})
}
