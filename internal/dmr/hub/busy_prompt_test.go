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

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	busyTalker    = listedRadio
	busyTalkerRun = uint(10)
	busyKeyUp     = uint(20)
)

// busyEnv has the talker busyTalker on ruleReceiver and the radio
// promptRadio on ruleSender, which comes in from the server source and
// gets its bursts on out, both on talkgroup 1 on TS1, under the first
// contention setting.
type busyEnv struct {
	*promptEnv
	source string
	out    *hub.ServerHandle
}

func busySetup(t *testing.T, e *promptEnv, source string, role hub.ServerRole, spoken bool) *busyEnv {
	t.Helper()
	b := &busyEnv{promptEnv: e, source: source, out: e.srv}
	if source != models.RepeaterTypeMMDVM {
		if role == hub.RoleRepeater {
			require.NoError(t, e.db.Model(&models.Repeater{}).Where("id = ?", ruleSender).Update("type", source).Error)
			e.hub.ForgetCachedRepeater(ruleSender)
			b.out = e.hub.RegisterServer(hub.ServerConfig{Name: source, Role: role})
		} else if _, isPlugin := plugins.PluginOfSource(source); !isPlugin {
			e.hub.RegisterServer(hub.ServerConfig{Name: source, Role: role})
		}
	}
	c := unlockConfig()
	c.DMR.Routing.SpokenRefusals = spoken
	c.DMR.Routing.Contention = config.ContentionFirst
	e.hub.SetConfig(c)
	return b
}

func busyBurst(src, rpt, stream uint, end bool) models.Packet {
	p := makeVoicePacket(1, stream, true, false)
	if end {
		p = makeVoiceTermPacket(1, stream, true, false)
	}
	p.Src, p.Repeater = src, rpt
	return p
}

// await reads ch until repeater rpt gets the burst of stream that end
// says.
func await(t *testing.T, ch <-chan hub.RoutedPacket, rpt, stream uint, end bool) {
	t.Helper()
	isEnd := func(p models.Packet) bool {
		return p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm
	}
	got := testutils.Collect(ch, failAfter, func(rps []hub.RoutedPacket) bool {
		rp := rps[len(rps)-1]
		return rp.RepeaterID == rpt && rp.Packet.StreamID == stream && isEnd(rp.Packet) == end
	})
	require.NotEmpty(t, got)
	last := got[len(got)-1]
	require.True(t, last.RepeaterID == rpt && last.Packet.StreamID == stream && isEnd(last.Packet) == end,
		"repeater %d did not get the burst of stream %d", rpt, stream)
}

// talkerKeysUp has busyTalker start a call that reaches the timeslot of
// ruleSender, so that it holds it.
func (b *busyEnv) talkerKeysUp(t *testing.T) {
	t.Helper()
	b.hub.RoutePacket(context.Background(), busyBurst(busyTalker, ruleReceiver, busyTalkerRun, false), models.RepeaterTypeMMDVM)
	await(t, b.out.Packets, ruleSender, busyTalkerRun, false)
}

// radioKeysUp has promptRadio send a call on the held timeslot of
// ruleSender, which reaches ruleReceiver.
func (b *busyEnv) radioKeysUp(t *testing.T) {
	t.Helper()
	for _, end := range []bool{false, true} {
		b.hub.RoutePacket(context.Background(), busyBurst(promptRadio, ruleSender, busyKeyUp, end), b.source)
	}
	await(t, b.srv.Packets, ruleReceiver, busyKeyUp, true)
}

func (b *busyEnv) talkerEnds(t *testing.T) {
	t.Helper()
	b.hub.RoutePacket(context.Background(), busyBurst(busyTalker, ruleReceiver, busyTalkerRun, true), models.RepeaterTypeMMDVM)
	await(t, b.out.Packets, ruleSender, busyTalkerRun, true)
}

// busyPrompt advances the clock until every burst of "Channel busy." went
// out, and checks that ruleSender got all of it, to promptRadio on TS1.
func (b *busyEnv) busyPrompt(t *testing.T) {
	t.Helper()
	want, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clip: prompts.ChannelBusy, Codec: transcode.CodecAMBE, Src: 9999, Dst: promptRadio})
	require.NoError(t, err)
	b.clk.Advance(prompts.Duration(len(want)))
	var got []models.Packet
	testutils.Collect(b.out.Packets, failAfter, func(rps []hub.RoutedPacket) bool {
		if rp := rps[len(rps)-1]; rp.RepeaterID == ruleSender && rp.Packet.Src == 9999 {
			got = append(got, rp.Packet)
		}
		return len(got) >= len(want)
	})
	require.Len(t, got, len(want), "the prompt did not arrive")
	for _, p := range got {
		assert.Equal(t, promptRadio, p.Dst)
		assert.False(t, p.GroupCall)
		assert.False(t, p.Slot)
	}
	assertClip(t, prompts.ChannelBusy, got)
}

// TestChannelBusyWhenTheRadioKeysUpOnAHeldTimeslot has a radio key up on
// the timeslot of its hotspot or repeater while the call of another talker
// holds it. The radio hears "Channel busy." once, a second after its call
// ends, and the talker hears nothing.
func TestChannelBusyWhenTheRadioKeysUpOnAHeldTimeslot(t *testing.T) {
	t.Parallel()
	for _, source := range []string{models.RepeaterTypeMMDVM, models.RepeaterTypeIPSC, models.RepeaterTypeModem} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
				b := busySetup(t, e, source, hub.RoleRepeater, true)
				b.talkerKeysUp(t)
				b.radioKeysUp(t)
				assert.True(t, b.hub.PromptQueued(ruleSender, false), "no prompt waits for the radio")
				b.talkerEnds(t)
				assert.False(t, b.hub.PromptQueued(ruleReceiver, false), "a prompt waits for the talker")

				b.clk.Advance(time.Second)
				b.busyPrompt(t)
				assert.False(t, b.hub.PromptQueued(ruleSender, false))

				for _, end := range []bool{false, true} {
					b.hub.RoutePacket(context.Background(), busyBurst(promptRadio, ruleSender, busyKeyUp, end), b.source)
				}
				await(t, b.srv.Packets, ruleReceiver, busyKeyUp, true)
				assert.False(t, b.hub.PromptQueued(ruleSender, false), "late bursts of the stream queue a second prompt")
			})
		})
	}
}

// TestNoChannelBusyWithoutSpokenRefusals has the same contention with the
// spoken refusals off.
func TestNoChannelBusyWithoutSpokenRefusals(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, false, func(t *testing.T, e *promptEnv) {
		b := busySetup(t, e, models.RepeaterTypeMMDVM, hub.RoleRepeater, false)
		b.talkerKeysUp(t)
		b.radioKeysUp(t)
		b.talkerEnds(t)
		assert.False(t, b.hub.PromptQueued(ruleSender, false))
		assert.False(t, b.hub.PromptQueued(ruleReceiver, false))
	})
}

// TestNoChannelBusyUnderMix has the same contention with the mix setting,
// which holds no timeslot.
func TestNoChannelBusyUnderMix(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		b := busySetup(t, e, models.RepeaterTypeMMDVM, hub.RoleRepeater, true)
		c := unlockConfig()
		c.DMR.Routing.SpokenRefusals = true
		e.hub.SetConfig(c)
		b.talkerKeysUp(t)
		b.radioKeysUp(t)
		assert.False(t, b.hub.PromptQueued(ruleSender, false))
	})
}

// TestNoChannelBusyForPeersAndPlugins sends the second call from an
// OpenBridge peer and from a plugin, with the ID of the held repeater.
func TestNoChannelBusyForPeersAndPlugins(t *testing.T) {
	t.Parallel()
	for name, source := range map[string]string{"openbridge": "openbridge", "plugin": plugins.SourceName(7)} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
				b := busySetup(t, e, source, hub.RolePeer, true)
				b.talkerKeysUp(t)
				b.radioKeysUp(t)
				b.talkerEnds(t)
				assert.False(t, b.hub.PromptQueued(ruleSender, false))
			})
		})
	}
}

// TestAPromptWaitsForTheCallThatHoldsTheTimeslot keeps the talker on the
// air after the refused call ends: the prompt waits until a second passes
// with no burst of the call that holds the timeslot.
func TestAPromptWaitsForTheCallThatHoldsTheTimeslot(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		b := busySetup(t, e, models.RepeaterTypeMMDVM, hub.RoleRepeater, true)
		b.talkerKeysUp(t)
		b.radioKeysUp(t)
		b.clk.Advance(900 * time.Millisecond)
		b.hub.RoutePacket(context.Background(), busyBurst(busyTalker, ruleReceiver, busyTalkerRun, false), models.RepeaterTypeMMDVM)
		await(t, b.out.Packets, ruleSender, busyTalkerRun, false)
		b.clk.Advance(100 * time.Millisecond)
		assert.True(t, b.hub.PromptQueued(ruleSender, false), "the prompt played over the call that holds the timeslot")
		assert.Empty(t, b.sentToSender())

		b.talkerEnds(t)
		b.clk.Advance(900 * time.Millisecond)
		b.busyPrompt(t)
	})
}
