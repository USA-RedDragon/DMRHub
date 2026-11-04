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
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/airauth"
	"github.com/USA-RedDragon/DMRHub/internal/clock"
	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/dmrconst"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/hub"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/transcode"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/dmrgo/v2/layer2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const promptRadio = otherUser

type promptEnv struct {
	*rulesEnv
	clk  *clock.Fake
	ends chan models.Packet
}

func forPromptBackends(t *testing.T, spoken bool, f func(t *testing.T, e *promptEnv)) {
	t.Helper()
	for _, be := range []testutils.Backend{testutils.SQLiteMemoryBackend(), testutils.PostgresRedisBackend(), testutils.MySQLBackend(), testutils.MariaDBBackend()} {
		t.Run(be.Name, func(t *testing.T) {
			t.Parallel()
			clk := clock.NewFake(time.Now())
			e := &promptEnv{rulesEnv: rulesSetup(t, be, hub.WithClock(clk)), clk: clk, ends: make(chan models.Packet, 64)}
			c := unlockConfig()
			c.DMR.Routing.SpokenRefusals = spoken
			e.hub.SetConfig(c)
			e.hub.SetUndeliverableFunc(func(p models.Packet, _, _ string) {
				if p.FrameType == dmrconst.FrameDataSync && dmrconst.DataType(p.DTypeOrVSeq) == dmrconst.DTypeVoiceTerm {
					e.ends <- p
				}
			})
			f(t, e)
		})
	}
}

// refusedCall sends a voice burst and the terminator of a group call from
// promptRadio to talkgroup 1 through ruleSender, and waits for the hub to refuse
// the terminator.
func (e *promptEnv) refusedCall(t *testing.T, stream uint) {
	t.Helper()
	for _, p := range []models.Packet{makeVoicePacket(1, stream, true, false), makeVoiceTermPacket(1, stream, true, false)} {
		p.Src, p.Repeater = promptRadio, ruleSender
		e.hub.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
	}
	got := testutils.Collect(e.ends, failAfter, func(got []models.Packet) bool { return len(got) > 0 })
	require.Len(t, got, 1, "the hub did not refuse the terminator")
	require.Equal(t, stream, got[0].StreamID)
}

func (e *promptEnv) sentToSender() []models.Packet {
	var out []models.Packet
	for {
		select {
		case rp := <-e.srv.Packets:
			if rp.RepeaterID == ruleSender {
				out = append(out, rp.Packet)
			}
		default:
			return out
		}
	}
}

// prompt advances the clock until every burst of the prompt clip went out,
// and returns the bursts that reach ruleSender.
func (e *promptEnv) prompt(t *testing.T, clip string) []models.Packet {
	t.Helper()
	want, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clip: clip, Codec: transcode.CodecAMBE, Src: 9999, Dst: promptRadio})
	require.NoError(t, err)
	e.clk.Advance(prompts.Duration(len(want)))
	got := e.collect(t, len(want), func(rp hub.RoutedPacket) bool { return rp.RepeaterID == ruleSender }, clip)
	for _, p := range got {
		assert.Equal(t, uint(9999), p.Src)
		assert.Equal(t, promptRadio, p.Dst)
		assert.False(t, p.GroupCall)
		assert.False(t, p.Slot)
	}
	assertClip(t, clip, got)
	return got
}

func TestARefusedCallGetsItsPromptOnceASecondAfterItEnds(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), blockTG1())
		e.refusedCall(t, 1)
		e.clk.Advance(time.Second - time.Millisecond)
		assert.Empty(t, e.sentToSender(), "no prompt while the radio may still transmit")
		e.clk.Advance(time.Millisecond)
		e.prompt(t, prompts.TalkgroupBlocked)

		e.refusedCall(t, 1)
		e.clk.Advance(10 * time.Second)
		e.refusedCall(t, 3)
		e.clk.Advance(time.Second)
		e.prompt(t, prompts.TalkgroupBlocked)
		assert.Empty(t, e.sentToSender(), "late bursts of a stream that got its prompt get no second one")
	})
}

func TestAPromptWaitsForTheLastRefusedBurst(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), blockTG1())
		e.refusedCall(t, 1)
		e.clk.Advance(900 * time.Millisecond)
		e.refusedCall(t, 1)
		e.clk.Advance(900 * time.Millisecond)
		assert.Empty(t, e.sentToSender())
		e.clk.Advance(100 * time.Millisecond)
		e.prompt(t, prompts.TalkgroupBlocked)
	})
}

func TestNoPromptWhenSpokenRefusalsAreOff(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, false, func(t *testing.T, e *promptEnv) {
		e.setRules(t, ruleSender, models.TalkgroupRuleKinds(), blockTG1())
		e.refusedCall(t, 1)
		e.clk.Advance(time.Minute)
		e.hub.SetConfig(func() config.Config { c := unlockConfig(); c.DMR.Routing.SpokenRefusals = true; return c }())
		e.refusedCall(t, 3)
		e.clk.Advance(time.Second)
		got := e.prompt(t, prompts.TalkgroupBlocked)
		assert.NotEmpty(t, got, "the switch works when on")
		assert.Empty(t, e.sentToSender(), "the call while it was off got no prompt")
	})
}

func TestEachRefusalHasItsOwnPrompt(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		e.setAccess(t, 1, true, ruleReceiver)
		e.refusedCall(t, 1)
		e.clk.Advance(time.Second)
		e.prompt(t, prompts.TalkgroupRestricted)
	})
}

type signalledReplies struct {
	inner hub.UnlockReplies
	done  chan struct{}
}

func (r signalledReplies) Locked(u hub.Unlock) {
	r.inner.Locked(u)
	r.done <- struct{}{}
}

func (r signalledReplies) Tried(u hub.Unlock, res airauth.Result) {
	r.inner.Tried(u, res)
	r.done <- struct{}{}
}

func TestTheOnAirCodeRepliesAreSpoken(t *testing.T) {
	t.Parallel()
	forPromptBackends(t, true, func(t *testing.T, e *promptEnv) {
		r := signalledReplies{inner: e.hub.PromptReplies(), done: make(chan struct{}, 8)}
		e.hub.SetUnlockReplies(r)
		secret, now := enrollOnAir(t, e.hub)
		wait := func() {
			t.Helper()
			got := testutils.Collect(r.done, failAfter, func(got []struct{}) bool { return len(got) > 0 })
			require.Len(t, got, 1)
		}

		for _, p := range []models.Packet{makeVoicePacket(1, 1, true, false), makeVoiceTermPacket(1, 1, true, false)} {
			p.Src, p.Repeater = listedRadio, ruleSender
			e.hub.RoutePacket(context.Background(), p, models.RepeaterTypeMMDVM)
		}
		wait()
		end := testutils.Collect(e.ends, failAfter, func(got []models.Packet) bool { return len(got) > 0 })
		require.Len(t, end, 1)
		e.clk.Advance(time.Second)
		e.promptTo(t, listedRadio, prompts.CodeNeeded)

		e.sendCode(t, listedRadio, airauth.Code(secret, airauth.Step(now)+1)+1)
		wait()
		e.clk.Advance(time.Second)
		e.promptTo(t, listedRadio, prompts.WrongCode)

		e.sendCode(t, listedRadio, airauth.Code(secret, airauth.Step(now)+1))
		wait()
		e.clk.Advance(time.Second)
		e.promptTo(t, listedRadio, prompts.Unlocked)
	})
}

func (e *promptEnv) promptTo(t *testing.T, radio uint, clip string) {
	t.Helper()
	want, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clip: clip, Codec: transcode.CodecAMBE, Src: 9999, Dst: radio})
	require.NoError(t, err)
	e.clk.Advance(prompts.Duration(len(want)))
	got := e.collect(t, len(want), func(rp hub.RoutedPacket) bool {
		return rp.RepeaterID == ruleSender && rp.Packet.Src == 9999
	}, clip)
	for _, p := range got {
		assert.Equal(t, radio, p.Dst)
	}
	assertClip(t, clip, got)
}

func assertClip(t *testing.T, clip string, got []models.Packet) {
	t.Helper()
	want, err := prompts.Frames(prompts.DefaultVoice, clip, transcode.CodecAMBE)
	require.NoError(t, err)
	var slots [][9]byte
	for _, p := range got {
		if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
			s := layer2.RawBurst(p.DMRData).VoiceSlots()
			slots = append(slots, s[:]...)
		}
	}
	require.GreaterOrEqual(t, len(slots), len(want), clip)
	assert.Equal(t, want, slots[:len(want)], clip)
}

// collect waits for n bursts on the server that keep says to keep, the
// bursts of the prompt clip.
func (e *promptEnv) collect(t *testing.T, n int, keep func(hub.RoutedPacket) bool, clip string) []models.Packet {
	t.Helper()
	var got []models.Packet
	testutils.Collect(e.srv.Packets, failAfter, func(rps []hub.RoutedPacket) bool {
		if rp := rps[len(rps)-1]; keep(rp) {
			got = append(got, rp.Packet)
		}
		return len(got) >= n
	})
	require.Len(t, got, n, "the prompt %s did not arrive", clip)
	return got
}

func TestAPluginPromptPlaysItsClipsToTheRadioASecondAfterItAsks(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h, _, srv := pluginHub(t)
		ctx := context.Background()
		hear(t, h, answerRadio, answerRepeater, 50)
		time.Sleep(time.Minute)
		synctest.Wait()
		for len(srv.Packets) > 0 {
			<-srv.Packets
		}

		clips := []string{prompts.WordTimeslot, prompts.Digit(1), prompts.WordDynamic, prompts.WordNone}
		require.NoError(t, h.PlayPrompt(ctx, answerService, answerRadio, false, clips, 51), "whether or not spoken refusals are on")
		time.Sleep(time.Second - time.Millisecond)
		synctest.Wait()
		assert.Empty(t, srv.Packets, "no prompt while the radio may still transmit")
		want, err := prompts.Build(prompts.Call{Voice: prompts.DefaultVoice, Clips: clips, Codec: transcode.CodecAMBE, Src: answerService, Dst: answerRadio})
		require.NoError(t, err)
		time.Sleep(time.Millisecond + prompts.Duration(len(want)))
		synctest.Wait()
		var slots, wantSlots [][9]byte
		got := 0
		for len(srv.Packets) > 0 {
			rp := <-srv.Packets
			p := rp.Packet
			got++
			assert.Equal(t, answerRepeater, rp.RepeaterID)
			assert.Equal(t, answerService, p.Src)
			assert.Equal(t, answerRadio, p.Dst)
			assert.False(t, p.GroupCall)
			assert.False(t, p.Slot)
			if p.FrameType == dmrconst.FrameVoice || p.FrameType == dmrconst.FrameVoiceSync {
				s := layer2.RawBurst(p.DMRData).VoiceSlots()
				slots = append(slots, s[:]...)
			}
		}
		assert.Equal(t, len(want), got)
		for _, c := range clips {
			f, err := prompts.Frames(prompts.DefaultVoice, c, transcode.CodecAMBE)
			require.NoError(t, err)
			wantSlots = append(wantSlots, f...)
		}
		require.GreaterOrEqual(t, len(slots), len(wantSlots))
		assert.Equal(t, wantSlots, slots[:len(wantSlots)], "the clips, in order")

		require.NoError(t, h.PlayPrompt(ctx, answerService, answerRadio, false, clips, 51))
		time.Sleep(time.Minute)
		synctest.Wait()
		assert.Empty(t, srv.Packets, "one prompt for each stream of the radio")

		require.ErrorIs(t, h.PlayPrompt(ctx, answerService, answerRadio, false, []string{"nope"}, 52), plugins.ErrUnknownClip)
		require.ErrorIs(t, h.PlayPrompt(ctx, answerService, 1000999, false, clips, 53), plugins.ErrRadioNotFound)
	})
}
