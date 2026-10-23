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

// TestPrivateTrafficToAnUnregisteredRadioGoesToPeers sends a private call
// and private data to a radio ID that is not a DMRHub user. No local
// repeater gets them, but an OpenBridge peer does, so calls and SMS can
// reach radios on other networks.
func TestPrivateTrafficToAnUnregisteredRadioGoesToPeers(t *testing.T) {
	t.Parallel()
	forAllBackends(t, func(t *testing.T, stack *testutils.IntegrationStack) {
		s := startEnhancedStack(t, stack)
		const unregistered = 3999999

		for i, sent := range []struct {
			frame dmrconst.FrameType
			value uint
		}{
			{dmrconst.FrameDataSync, uint(enums.DataTypeVoiceLCHeader)},
			{dmrconst.FrameDataSync, uint(enums.DataTypeDataHeader)},
		} {
			p := makeBurst(1000001, unregistered, uint(9500+i), false, sent.frame, sent.value, 0)
			require.NoError(t, s.mc.SendDMRD(p))
			got := s.peer.Drain(drainWait)
			if assert.Len(t, got, 1, "burst %d must reach the peer", i) {
				assert.Equal(t, uint(unregistered), got[0].Dst)
				assert.False(t, got[0].GroupCall)
				assert.Equal(t, p.DMRData, got[0].DMRData)
			}
		}
		assert.Empty(t, s.mc.Drain(300*time.Millisecond), "no local repeater gets private traffic to a radio that is not a user")
	})
}
