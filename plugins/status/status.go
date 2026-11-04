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

// Package status is a DMRHub plugin that tells a radio the talkgroups of
// the timeslot it called on: by voice for a private voice call to its ID,
// and by text for a text to its ID.
package status

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/sdk/plugin"
	pluginv1 "github.com/USA-RedDragon/DMRHub/sdk/plugin/v1"
)

// The names of the hub's clips that the spoken answer uses.
const (
	ClipTimeslot  = "word-timeslot"
	ClipTalkgroup = "word-talkgroup"
	ClipLinked    = "word-linked"
	ClipStatic    = "word-static"
	ClipDynamic   = "word-dynamic"
	ClipNone      = "word-none"
)

const (
	// maxClips is the most clips of one prompt that the hub plays.
	maxClips = 64
	// answerWait bounds the requests of one answer.
	answerWait = 30 * time.Second
	// forget is how long the plugin remembers a call or a text that it
	// answered, so that a repeated terminator or a Text that comes on two
	// streams gets one answer.
	forget = 10 * time.Minute
)

// ClipDigit returns the name of the clip of the digit d, 0 to 9.
func ClipDigit(d int) string {
	return "digit-" + strconv.Itoa(d)
}

// Hub is what the plugin asks the hub. A *plugin.Conn is a Hub.
type Hub interface {
	Repeater(ctx context.Context, id uint32) (*pluginv1.RepeaterInfo, error)
	PlayPrompt(ctx context.Context, p *pluginv1.PlayPrompt) error
	ReplyText(ctx context.Context, source uint32, text *pluginv1.Text, body string) (string, error)
}

// Status answers the calls and texts to its IDs.
type Status struct {
	hub Hub
	now func() time.Time

	mu       sync.Mutex
	answered map[answerKey]time.Time
	wg       sync.WaitGroup
}

type answerKey struct {
	text bool
	id   uint64
}

// New returns a Status that asks hub.
func New(hub Hub) *Status {
	return &Status{hub: hub, now: time.Now, answered: map[answerKey]time.Time{}}
}

// Wait waits for the answers that are under way.
func (s *Status) Wait() {
	s.wg.Wait()
}

// first reports whether k gets its answer now, the first time it comes.
func (s *Status) first(k answerKey) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	for old, at := range s.answered {
		if now.Sub(at) > forget {
			delete(s.answered, old)
		}
	}
	if _, done := s.answered[k]; done {
		return false
	}
	s.answered[k] = now
	return true
}

// Burst takes a burst to one of the plugin's IDs. At the terminator of a
// private voice call, it asks the hub to play the status of the caller's
// timeslot to the caller. It ignores group calls and data.
func (s *Status) Burst(ctx context.Context, b *pluginv1.Burst) {
	if b.GetGroupCall() || !plugin.IsTerminator(b) || b.GetRepeater() == 0 || !s.first(answerKey{id: uint64(b.GetStreamId())}) {
		return
	}
	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), answerWait)
		defer cancel()
		slot, ok := s.slot(ctx, b.GetRepeater(), b.GetSlot())
		if !ok {
			return
		}
		err := s.hub.PlayPrompt(ctx, &pluginv1.PlayPrompt{
			Source: b.GetDestination(), Radio: b.GetSource(), Slot: b.GetSlot(), Clips: Clips(slot), StreamId: b.GetStreamId(),
		})
		if err != nil {
			slog.Warn("Status: cannot play the status", "radio", b.GetSource(), "error", err)
		}
	})
}

// Text takes a text to one of the plugin's IDs and answers it with the
// status of the caller's timeslot, in the format of the text. The hub
// sends no answer in a format that it cannot build again exactly, such as
// a UDT. It ignores group texts.
func (s *Status) Text(ctx context.Context, t *pluginv1.Text) {
	if t.GetGroup() || t.GetRepeater() == 0 || !s.first(answerKey{text: true, id: t.GetId()}) {
		return
	}
	s.wg.Go(func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), answerWait)
		defer cancel()
		slot, ok := s.slot(ctx, t.GetRepeater(), t.GetSlot())
		if !ok {
			return
		}
		_, err := s.hub.ReplyText(ctx, t.GetDestination(), t, Words(slot))
		if errors.Is(err, plugin.ErrInvalid) {
			_, err = s.hub.ReplyText(ctx, t.GetDestination(), t, Short(slot))
		}
		switch {
		case errors.Is(err, plugin.ErrNoFormat):
			slog.Info("Status: no answer to a text in a format that the hub cannot answer in", "radio", t.GetSource(), "format", t.GetFormat())
		case err != nil:
			slog.Warn("Status: cannot answer a text", "radio", t.GetSource(), "error", err)
		}
	})
}

func (s *Status) slot(ctx context.Context, repeater, slot uint32) (*pluginv1.SlotInfo, bool) {
	info, err := s.hub.Repeater(ctx, repeater)
	if err != nil {
		slog.Warn("Status: cannot read the talkgroups of a repeater", "repeater", repeater, "error", err)
		return nil, false
	}
	for _, si := range info.GetSlots() {
		if si.GetSlot() == slot {
			return si, true
		}
	}
	return &pluginv1.SlotInfo{Slot: slot}, true
}

// Clips returns the clips that say the status of slot: "Timeslot two.
// Linked: talkgroup three one zero zero, static. Dynamic: none." When the
// static talkgroups take more clips than a prompt holds, it says the
// first ones.
func Clips(slot *pluginv1.SlotInfo) []string {
	tail := []string{ClipDynamic}
	if d := slot.DynamicTalkgroup; d != nil {
		tail = appendTalkgroup(tail, *d)
	} else {
		tail = append(tail, ClipNone)
	}
	out := append(digits([]string{ClipTimeslot}, slot.GetSlot()), ClipLinked)
	statics := slot.GetStaticTalkgroups()
	if len(statics) == 0 {
		out = append(out, ClipNone)
	}
	for _, tg := range statics {
		next := append(appendTalkgroup(nil, tg), ClipStatic)
		if len(out)+len(next)+len(tail) > maxClips {
			break
		}
		out = append(out, next...)
	}
	return append(out, tail...)
}

func appendTalkgroup(out []string, tg uint32) []string {
	return digits(append(out, ClipTalkgroup), tg)
}

func digits(out []string, n uint32) []string {
	for _, c := range strconv.FormatUint(uint64(n), 10) {
		out = append(out, ClipDigit(int(c-'0')))
	}
	return out
}

// Words returns the status of slot as the text "Timeslot 2. Linked:
// talkgroup 3100, static. Dynamic: none."
func Words(slot *pluginv1.SlotInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Timeslot %d. Linked: ", slot.GetSlot())
	statics := slot.GetStaticTalkgroups()
	if len(statics) == 0 {
		b.WriteString("none")
	}
	for i, tg := range statics {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "talkgroup %d, static", tg)
	}
	b.WriteString(". Dynamic: ")
	if d := slot.DynamicTalkgroup; d != nil {
		fmt.Fprintf(&b, "talkgroup %d.", *d)
	} else {
		b.WriteString("none.")
	}
	return b.String()
}

// Short returns the status of slot as the short text "TS2 S:3100 D:-",
// for a format that cannot carry Words.
func Short(slot *pluginv1.SlotInfo) string {
	var b strings.Builder
	fmt.Fprintf(&b, "TS%d S:", slot.GetSlot())
	statics := slot.GetStaticTalkgroups()
	if len(statics) == 0 {
		b.WriteString("-")
	}
	for i, tg := range statics {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.FormatUint(uint64(tg), 10))
	}
	b.WriteString(" D:")
	if d := slot.DynamicTalkgroup; d != nil {
		b.WriteString(strconv.FormatUint(uint64(*d), 10))
	} else {
		b.WriteString("-")
	}
	return b.String()
}

// Serve answers the calls and texts that come on conn until it ends. Give
// it to plugin.Run.
func Serve(ctx context.Context, conn *plugin.Conn) error {
	s := New(conn)
	defer s.Wait()
	if conn.Has(pluginv1.Feature_FEATURE_TEXTS) {
		if err := conn.Subscribe(&pluginv1.Subscribe{Texts: true}); err != nil {
			return fmt.Errorf("cannot subscribe to texts: %w", err)
		}
	}
	for {
		m, err := conn.Recv()
		if err != nil {
			return fmt.Errorf("cannot receive: %w", err)
		}
		switch msg := m.GetMessage().(type) {
		case *pluginv1.ConnectResponse_Burst:
			s.Burst(ctx, msg.Burst)
		case *pluginv1.ConnectResponse_Text:
			s.Text(ctx, msg.Text)
		case *pluginv1.ConnectResponse_Refusal:
			slog.Warn("Status: the hub refused a burst", "stream", msg.Refusal.GetStreamId(), "reason", msg.Refusal.GetReason())
		case *pluginv1.ConnectResponse_Welcome, *pluginv1.ConnectResponse_Answer:
		default:
			slog.Debug("Status: ignoring a message it does not know", "message", m)
		}
	}
}
