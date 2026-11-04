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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestDMRHubRadioHearsChannelBusy has a DMRHub Radio over WebTransport key
// up talkgroup 1 while the call of a hotspot holds TS1 of the browser
// radio. The browser radio hears "Channel busy." as a private call from
// the service ID once the call that held its timeslot ends, and the
// hotspot hears no prompt.
func TestDMRHubRadioHearsChannelBusy(t *testing.T) {
	t.Parallel()
	forAllBackendsWith(t, []testutils.Backend{testutils.WebTransportOn()}, func(t *testing.T, stack *testutils.IntegrationStack) {
		const talker, radio, talkerStream, radioStream = 1000001, 1000002, 8401, 8402
		stack.SeedUser(t, talker, "N0CALL")
		stack.SeedUser(t, radio, "N1CALL")
		stack.SeedTalkgroup(t, 1, "TG1")
		stack.SeedMMDVMRepeater(t, 100001, talker, testPassword)
		stack.SeedMMDVMRepeater(t, 100004, radio, testPassword)
		stack.AssignTS1StaticTG(t, 100001, 1)
		stack.AssignTS1StaticTG(t, 100004, 1)
		routingStack(t, stack, config.Routing{Contention: config.ContentionFirst, SpokenRefusals: true})
		stack.StartServers(t)

		browser, err := testutils.NewWebTransportPeer(100004, testPassword)
		require.NoError(t, err)
		require.NoError(t, browser.Connect(stack.WebTransportAddr))
		t.Cleanup(browser.Close)
		require.NoError(t, browser.WaitReady(handshakeWait))
		hotspot := testutils.NewMMDVMClient(100001, "", testPassword)
		require.NoError(t, hotspot.Connect(stack.MMDVMAddr))
		t.Cleanup(hotspot.Close)
		require.NoError(t, hotspot.WaitReady(handshakeWait))

		var heard []models.Packet
		got := func(pkts []models.Packet, stream uint, end bool) bool {
			p := pkts[len(pkts)-1]
			return p.StreamID == stream && isTerminator(p) == end
		}
		require.NoError(t, hotspot.SendDMRD(makeGroupVoicePacket(talker, 1, talkerStream, false)))
		heard = append(heard, browser.Receive(deliverWait, func(p []models.Packet) bool { return got(p, talkerStream, false) })...)

		require.NoError(t, browser.SendDMRD(makeGroupVoicePacket(radio, 1, radioStream, false)))
		require.NoError(t, browser.SendDMRD(makeGroupVoiceTermPacket(radio, 1, radioStream, false)))
		toTalker := testutils.Collect(hotspot.ReceivedPackets(), deliverWait, func(p []models.Packet) bool { return got(p, radioStream, true) })
		require.NotEmpty(t, toTalker)
		require.True(t, got(toTalker, radioStream, true), "the call of the browser radio did not reach the hotspot")

		require.NoError(t, hotspot.SendDMRD(makeGroupVoiceTermPacket(talker, 1, talkerStream, false)))
		var prompt []models.Packet
		heard = append(heard, browser.Receive(deliverWait, func(pkts []models.Packet) bool {
			if p := pkts[len(pkts)-1]; p.Src == prompts.DefaultServiceID {
				prompt = append(prompt, p)
			}
			return len(prompt) > 0 && isTerminator(prompt[len(prompt)-1])
		})...)
		require.NotEmpty(t, prompt, "the browser radio got no prompt")
		require.True(t, isTerminator(prompt[len(prompt)-1]), "the prompt did not end")
		for _, p := range prompt {
			assert.Equal(t, uint(radio), p.Dst)
			assert.False(t, p.GroupCall)
			assert.False(t, p.Slot)
		}
		want, err := prompts.Frames(prompts.DefaultVoice, prompts.ChannelBusy, transcode.CodecAMBE)
		require.NoError(t, err)
		var slots [][9]byte
		for _, p := range prompt {
			if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
				s := layer2.RawBurst(p.DMRData).VoiceSlots()
				slots = append(slots, s[:]...)
			}
		}
		require.GreaterOrEqual(t, len(slots), len(want))
		assert.Equal(t, want, slots[:len(want)], "the browser radio did not hear Channel busy")
		assert.Positive(t, countTerminators(heard, talkerStream), "the browser radio did not get the end of the call that held its timeslot")

		for _, p := range append(toTalker, hotspot.Drain(drainWait)...) {
			assert.NotEqual(t, prompts.DefaultServiceID, p.Src, "the hotspot of the talker got a prompt")
		}
		for _, p := range browser.Drain(drainWait) {
			assert.NotEqual(t, prompts.DefaultServiceID, p.Src, "the browser radio got a second prompt")
		}
	})
}

func countTerminators(pkts []models.Packet, stream uint) int {
	n := 0
	for _, p := range pkts {
		if p.StreamID == stream && isTerminator(p) {
			n++
		}
	}
	return n
}
