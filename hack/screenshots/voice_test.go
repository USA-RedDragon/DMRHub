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

package main

import (
	"testing"
	"time"

	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestVoiceCallIsOneCall plays a demo call into a voice receiver. Its
// voice bursts carry the embedded LC of the call, so a radio hears one
// call from the source to the talkgroup, not a second call of ID 0 when
// the receiver reads the embedded LC.
func TestVoiceCallIsOneCall(t *testing.T) {
	t.Parallel()
	bursts, err := voiceCall(true, 3100, 3000003, 30)
	require.NoError(t, err)
	rv, err := voice.NewReceiver(&voice.ReceiverConfig{})
	require.NoError(t, err)
	var starts, ends []voice.Event
	for i, b := range bursts {
		var h layer2.Hint
		for _, e := range rv.Burst(time.Duration(i)*voiceBurstGap, &b.raw, h, nil) {
			if e.Kind == voice.EventCallStart {
				starts = append(starts, e)
			}
			if e.Kind == voice.EventCallEnd {
				ends = append(ends, e)
			}
		}
	}
	require.Len(t, starts, 1)
	require.Len(t, ends, 1)
	assert.Equal(t, uint32(3000003), starts[0].Call.Source)
	assert.Equal(t, uint32(3100), starts[0].Call.Target)
	assert.Equal(t, voice.EndTerminator, ends[0].Reason)
}
