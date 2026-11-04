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

package status_test

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/USA-RedDragon/DMRHub/internal/prompts"
	"github.com/USA-RedDragon/DMRHub/internal/testutils"
	"github.com/USA-RedDragon/DMRHub/plugins/status"
	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/protobuf/proto"
)

const (
	radio    = uint32(1000001)
	repeater = uint32(100001)
)

func tg(n uint32) *uint32 { return &n }

type fakeHub struct {
	mu      sync.Mutex
	info    *pluginv1.RepeaterInfo
	played  []*pluginv1.PlayPrompt
	replies []string
	formats []string
	errs    []error
}

func (f *fakeHub) Repeater(_ context.Context, id uint32) (*pluginv1.RepeaterInfo, error) {
	if id != f.info.GetRepeater() {
		return nil, plugin.ErrNotFound
	}
	return f.info, nil
}

func (f *fakeHub) PlayPrompt(_ context.Context, p *pluginv1.PlayPrompt) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.played = append(f.played, p)
	return nil
}

func (f *fakeHub) ReplyText(_ context.Context, source uint32, text *pluginv1.Text, body string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if source != text.GetDestination() {
		return "", fmt.Errorf("a reply from %d to a text to %d", source, text.GetDestination())
	}
	f.replies = append(f.replies, body)
	if len(f.errs) > 0 {
		err := f.errs[0]
		f.errs = f.errs[1:]
		return "", err
	}
	f.formats = append(f.formats, text.GetFormat())
	return text.GetFormat(), nil
}

func newHub() *fakeHub {
	return &fakeHub{info: &pluginv1.RepeaterInfo{Repeater: repeater, Callsign: "N0CALL", Slots: []*pluginv1.SlotInfo{
		{Slot: 1, StaticTalkgroups: []uint32{91, 3100}, DynamicTalkgroup: tg(9)},
		{Slot: 2, StaticTalkgroups: []uint32{3100}},
	}}}
}

func digits(s string) []string {
	out := make([]string, 0, len(s))
	for _, c := range s {
		out = append(out, status.ClipDigit(int(c-'0')))
	}
	return out
}

func seq(parts ...any) []string {
	var out []string
	for _, p := range parts {
		switch v := p.(type) {
		case string:
			out = append(out, v)
		case []string:
			out = append(out, v...)
		}
	}
	return out
}

func TestTheSpokenStatusSaysTheTalkgroupsOfTheTimeslot(t *testing.T) {
	t.Parallel()
	assert.Equal(t, seq(status.ClipTimeslot, digits("2"), status.ClipLinked, status.ClipTalkgroup, digits("3100"), status.ClipStatic, status.ClipDynamic, status.ClipNone),
		status.Clips(&pluginv1.SlotInfo{Slot: 2, StaticTalkgroups: []uint32{3100}}), "Timeslot two. Linked: talkgroup three one zero zero, static. Dynamic: none.")
	assert.Equal(t, seq(status.ClipTimeslot, digits("1"), status.ClipLinked,
		status.ClipTalkgroup, digits("91"), status.ClipStatic, status.ClipTalkgroup, digits("3100"), status.ClipStatic,
		status.ClipDynamic, status.ClipTalkgroup, digits("9")),
		status.Clips(newHub().info.GetSlots()[0]))
	assert.Equal(t, seq(status.ClipTimeslot, digits("1"), status.ClipLinked, status.ClipNone, status.ClipDynamic, status.ClipTalkgroup, digits("310")),
		status.Clips(&pluginv1.SlotInfo{Slot: 1, DynamicTalkgroup: tg(310)}))
}

func TestTheSpokenStatusFitsOnePrompt(t *testing.T) {
	t.Parallel()
	many := make([]uint32, 20)
	for i := range many {
		many[i] = 3100000 + uint32(i)
	}
	clips := status.Clips(&pluginv1.SlotInfo{Slot: 2, StaticTalkgroups: many, DynamicTalkgroup: tg(1234567)})
	assert.LessOrEqual(t, len(clips), 64)
	assert.Equal(t, seq(status.ClipDynamic, status.ClipTalkgroup, digits("1234567")), clips[len(clips)-9:], "the dynamic talkgroup is always said")
}

func TestEveryClipOfTheStatusIsAClipOfTheHub(t *testing.T) {
	t.Parallel()
	used := make([]string, 0, 16)
	used = append(used, status.ClipTimeslot, status.ClipTalkgroup, status.ClipLinked, status.ClipStatic, status.ClipDynamic, status.ClipNone)
	for d := range 10 {
		used = append(used, status.ClipDigit(d))
	}
	for _, c := range used {
		assert.True(t, slices.Contains(prompts.Clips(), c), "the hub has no clip %q", c)
	}
}

func TestTheWrittenStatusSaysTheSame(t *testing.T) {
	t.Parallel()
	slots := newHub().info.GetSlots()
	assert.Equal(t, "Timeslot 2. Linked: talkgroup 3100, static. Dynamic: none.", status.Words(slots[1]))
	assert.Equal(t, "Timeslot 1. Linked: talkgroup 91, static; talkgroup 3100, static. Dynamic: talkgroup 9.", status.Words(slots[0]))
	assert.Equal(t, "Timeslot 1. Linked: none. Dynamic: none.", status.Words(&pluginv1.SlotInfo{Slot: 1}))
	assert.Equal(t, "TS1 S:91,3100 D:9", status.Short(slots[0]))
	assert.Equal(t, "TS2 S:- D:-", status.Short(&pluginv1.SlotInfo{Slot: 2}))
}

func terminator(stream uint32) *pluginv1.Burst {
	return &pluginv1.Burst{
		Source: radio, Destination: status.ID, Slot: 2, StreamId: stream, Repeater: repeater,
		FrameType: pluginv1.FrameType_FRAME_TYPE_DATA_SYNC, DataTypeOrVoiceSequence: uint32(enums.DataTypeTerminatorWithLC),
	}
}

func TestAPrivateCallGetsTheSpokenStatusOfItsTimeslotOnce(t *testing.T) {
	t.Parallel()
	h := newHub()
	s := status.New(h)
	voice := terminator(41)
	voice.FrameType, voice.DataTypeOrVoiceSequence = pluginv1.FrameType_FRAME_TYPE_VOICE, 1
	s.Burst(t.Context(), voice)
	group := terminator(42)
	group.GroupCall = true
	s.Burst(t.Context(), group)
	s.Burst(t.Context(), terminator(43))
	s.Burst(t.Context(), terminator(43))
	s.Wait()
	require.Len(t, h.played, 1, "one answer, at the terminator of a private call")
	want := &pluginv1.PlayPrompt{Source: status.ID, Radio: radio, Slot: 2, StreamId: 43, Clips: status.Clips(h.info.GetSlots()[1])}
	assert.True(t, proto.Equal(want, h.played[0]), "got %v", h.played[0])
}

func text(id uint64, format string) *pluginv1.Text {
	return &pluginv1.Text{Id: id, Source: radio, Destination: status.ID, Slot: 1, Repeater: repeater, Format: format, Body: "STATUS"}
}

func TestATextGetsTheWrittenStatusInItsOwnFormatOnce(t *testing.T) {
	t.Parallel()
	for _, format := range []string{"TMS", "ETSI IP", "AnyTone", "Defined data", "Hytera"} {
		h := newHub()
		s := status.New(h)
		s.Text(t.Context(), text(7, format))
		s.Text(t.Context(), text(7, format))
		g := text(8, format)
		g.Group = true
		s.Text(t.Context(), g)
		s.Wait()
		assert.Equal(t, []string{status.Words(h.info.GetSlots()[0])}, h.replies, format)
		assert.Equal(t, []string{format}, h.formats, "a reply to the text, in its format")
	}
}

func TestAFormatThatCannotCarryTheStatusGetsTheShortOne(t *testing.T) {
	t.Parallel()
	h := newHub()
	h.errs = []error{fmt.Errorf("too long: %w", plugin.ErrInvalid)}
	s := status.New(h)
	s.Text(t.Context(), text(9, "Defined data"))
	s.Wait()
	slot := h.info.GetSlots()[0]
	assert.Equal(t, []string{status.Words(slot), status.Short(slot)}, h.replies)
}

func TestATextTheHubCannotAnswerInItsFormatGetsNoOtherFormat(t *testing.T) {
	t.Parallel()
	h := newHub()
	h.errs = []error{fmt.Errorf("a UDT: %w", plugin.ErrNoFormat)}
	s := status.New(h)
	s.Text(t.Context(), text(10, "UDT"))
	s.Wait()
	assert.Len(t, h.replies, 1, "one try, and no retry in another format")
	assert.Empty(t, h.formats)
}

// TestTheStatusIgnoresAMessageItDoesNotKnow gives the plugin a message of a
// newer hub, as an SDK that does not know it decodes it: with no type.
func TestTheStatusIgnoresAMessageItDoesNotKnow(t *testing.T) {
	t.Parallel()
	testutils.ServeIgnoresUnknownMessages(t, status.Serve)
}
