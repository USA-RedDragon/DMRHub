// SPDX-License-Identifier: MIT
// DMRHub plugin SDK
// Copyright (C) 2023-2026 Jacob McSwain
//
// Use of this source code is governed by the MIT license in sdk/LICENSE
// and proto/LICENSE.
//
// The source code is available at <https://github.com/USA-RedDragon/DMRHub>

package plugin_test

import (
	"context"
	"testing"
	"testing/synctest"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/vocoder"
	"github.com/USA-RedDragon/dmrgo/v2/voice"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAVoiceCallCarriesItsAddressesAndItsSlotsInOrder(t *testing.T) {
	t.Parallel()
	slots := make([][9]byte, 20)
	for i := range slots {
		slots[i] = [9]byte{byte(i + 1), 0x55, byte(i), 0, 0, 0, 0, 0, byte(i * 3)}
	}
	call := plugin.VoiceCall{Source: 5000, Destination: 1000001, Slot: 2, Stream: 42, ColorCode: 1}
	bursts, err := plugin.VoiceBursts(call, slots)
	require.NoError(t, err)

	first, last := bursts[0], bursts[len(bursts)-1]
	assert.Equal(t, pluginv1.FrameType_FRAME_TYPE_DATA_SYNC, first.GetFrameType())
	assert.Equal(t, uint32(enums.DataTypeVoiceLCHeader), first.GetDataTypeOrVoiceSequence())
	assert.True(t, plugin.IsTerminator(last))
	rx, err := voice.NewReceiver(&voice.ReceiverConfig{Lost: -1, Hang: -1})
	require.NoError(t, err)
	var got [][9]byte
	var heard voice.Call
	events := make([]voice.Event, 0, voice.MaxEvents)
	for i, b := range bursts {
		assert.True(t, plugin.IsVoice(b))
		assert.Equal(t, uint32(5000), b.GetSource())
		assert.Equal(t, uint32(1000001), b.GetDestination())
		assert.False(t, b.GetGroupCall())
		assert.Equal(t, uint32(2), b.GetSlot())
		assert.Equal(t, uint32(42), b.GetStreamId())
		assert.Equal(t, uint32(i&0xFF), b.GetSequence())
		d := plugin.DataOf(b)
		now := time.Duration(i) * plugin.BurstTime
		raw := d.Burst
		events = rx.Burst(now, &raw, d.Hint(now), events[:0])
		for j := range events {
			switch e := &events[j]; {
			case e.Kind == voice.EventVoice:
				s := e.Burst.VoiceSlots()
				got = append(got, s[:]...)
			case e.Call.Origin == voice.OriginHeader:
				heard = e.Call
			}
		}
	}
	assert.Equal(t, voice.Call{Target: 1000001, Source: 5000}, voice.Call{Group: heard.Group, Target: heard.Target, Source: heard.Source})
	require.GreaterOrEqual(t, len(got), len(slots))
	assert.Equal(t, slots, got[:len(slots)])
	for _, s := range got[len(slots):] {
		assert.Equal(t, vocoder.AMBESilenceSlot(), s, "silence pads the last superframe")
	}
	assert.Zero(t, len(got)%18, "whole superframes")
}

func TestAVoiceCallThatDMRCannotCarryIsRefused(t *testing.T) {
	t.Parallel()
	ok := plugin.VoiceCall{Source: 5000, Destination: 91, Group: true, Slot: 1, Stream: 1}
	_, err := plugin.VoiceBursts(ok, make([][9]byte, 3))
	require.NoError(t, err)
	for _, bad := range []plugin.VoiceCall{
		{Destination: 91, Slot: 1, Stream: 1},
		{Source: 5000, Slot: 1, Stream: 1},
		{Source: 5000, Destination: 1 << 24, Slot: 1, Stream: 1},
		{Source: 5000, Destination: 91, Slot: 3, Stream: 1},
		{Source: 5000, Destination: 91, Slot: 1},
		{Source: 5000, Destination: 91, Slot: 1, Stream: 1, ColorCode: 16},
	} {
		_, err := plugin.VoiceBursts(bad, make([][9]byte, 3))
		require.ErrorIs(t, err, plugin.ErrVoiceCall, bad)
	}
	_, err = plugin.VoiceBursts(ok, nil)
	require.ErrorIs(t, err, plugin.ErrVoiceCall)
}

func TestPlaySendsOneBurstEachBurstTime(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		start := time.Now()
		var at []time.Duration
		bursts := []*pluginv1.Burst{{}, {}, {}}
		require.NoError(t, plugin.Play(context.Background(), func(*pluginv1.Burst) error {
			at = append(at, time.Since(start))
			return nil
		}, bursts))
		assert.Equal(t, []time.Duration{0, plugin.BurstTime, 2 * plugin.BurstTime}, at)

		ctx, cancel := context.WithCancel(context.Background())
		sent := 0
		err := plugin.Play(ctx, func(*pluginv1.Burst) error {
			sent++
			cancel()
			return nil
		}, bursts)
		require.ErrorIs(t, err, context.Canceled)
		assert.Equal(t, 1, sent)
	})
}
