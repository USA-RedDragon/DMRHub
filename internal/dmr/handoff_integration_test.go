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

package dmr_test

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandoffResumesAListeningHotspot restarts the MMDVM server as in a
// handoff. A hotspot that only listens sends nothing after it, and its
// pings stay paused, so the first call on its talkgroup reaches it only
// when that call resumes its session.
func TestHandoffResumesAListeningHotspot(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		stack.SeedUser(t, 1000001, "N0CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, 1000001, testPassword)
		stack.SeedMMDVMRepeater(t, 100002, 1000001, testPassword)
		stack.AssignTS1StaticTG(t, 100002, 1)
		stack.StartServers(t)

		talker := testutils.NewMMDVMClient(100001, "", testPassword)
		listener := testutils.NewMMDVMClient(100002, "", testPassword)
		require.NoError(t, talker.Connect(stack.MMDVMAddr))
		require.NoError(t, listener.Connect(stack.MMDVMAddr))
		defer talker.Close()
		defer listener.Close()
		require.NoError(t, talker.WaitReady(handshakeWait))
		require.NoError(t, listener.WaitReady(handshakeWait))
		time.Sleep(settleDuration)
		listener.PausePings()

		stack.RestartMMDVM(t, testutils.RestartHandoff)

		require.NoError(t, talker.SendDMRD(makeBurst(1000001, 1, 9101, true, dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader), 0)))
		select {
		case got := <-listener.ReceivedPackets():
			assert.Equal(t, uint(9101), got.StreamID)
		case <-time.After(handshakeWait):
			t.Fatal("the listening hotspot got nothing after the handoff")
		}
	})
}
