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

package mmdvm

import (
	"bytes"
	"context"
	"log/slog"
	"slices"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/enums"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
	dmrdsession "github.com/USA-RedDragon/dmrgo/v2/transport/dmrd/dmrdsession"
)

// options applies the RPTO options text of the repeater of l. As in
// FreeDMR, a text with static talkgroups defines both slots: the list of
// each slot replaces the static talkgroups the repeater has, and a slot
// that the text does not name is cleared. A text with no static talkgroup
// key leaves them. The default dial of a slot (DIAL and its aliases) links
// its talkgroup as the dynamic talkgroup of the slot, as a voice call on
// it does, and 0 unlinks the slot. FreeDMR and DMR+ texts are both read as
// FreeDMR reads them, since FreeDMR takes the DMR+ keys too. DMRHub has no
// feature for the other options, so they are logged and ignored, as is a
// text that cannot be read.
func (s *Server) options(ctx context.Context, l *link, o dmrdsession.Output) {
	if s.byID[o.ID] != l || l.m.ID() != o.ID {
		slog.Info("Ignoring options that are not from the session of the repeater", "repeaterID", o.ID, "remoteAddr", l.ep.String())
		return
	}
	if l.moved {
		slog.Info("Ignoring options of a session that moved from another replica until it logs in", "repeaterID", o.ID, "remoteAddr", l.ep.String())
		return
	}
	text := o.Packet
	slog.Info("Repeater options", "repeaterID", o.ID, "options", string(text))
	if len(text) > dmrd.MaxFreeDMROptionsLen {
		slog.Info("Ignoring repeater options longer than FreeDMR reads", "repeaterID", o.ID, "length", len(text))
		return
	}
	id := uint(o.ID)
	parsed, err := dmrd.ParseFreeDMROptions(text)
	if err != nil {
		slog.Info("Repeater options have a value that is not valid", "repeaterID", id, "error", err)
	}
	logIgnoredOptions(id, text, &parsed)
	text = append([]byte(nil), text...)
	s.records.Do(id, func() {
		statics := s.applyStatics(ctx, id, text)
		dial1 := s.dial(ctx, id, false, parsed.DialTS1)
		dial2 := s.dial(ctx, id, true, parsed.DialTS2)
		if !statics && !dial1 && !dial2 {
			slog.Info("No repeater options that DMRHub applies", "repeaterID", id)
		}
	})
}

// applyStatics saves the static talkgroups that text sets and reloads the
// subscriptions of the repeater. It reports whether it changed them.
func (s *Server) applyStatics(ctx context.Context, id uint, text []byte) bool {
	set1, bad1 := staticOption(text, enums.Slot1)
	set2, bad2 := staticOption(text, enums.Slot2)
	if !set1 && !set2 {
		return false
	}
	ts1 := s.staticTalkgroups(id, text, enums.Slot1, set1, bad1)
	ts2 := s.staticTalkgroups(id, text, enums.Slot2, set2, bad2)
	if ts1 == nil && ts2 == nil {
		return false
	}
	if err := models.SetRepeaterStaticTalkgroups(s.DB, id, ts1, ts2); err != nil {
		slog.Error("Error saving the static talkgroups of the repeater options", "repeaterID", id, "error", err)
		return false
	}
	slog.Info("Applied static talkgroups of repeater options", "repeaterID", id, "ts1Static", talkgroupIDs(ts1), "ts2Static", talkgroupIDs(ts2))
	if s.hub != nil {
		s.hub.ReloadRepeater(ctx, id)
	}
	return true
}

// dial applies the default dial v of a slot, slot2 true for TS2: a
// talkgroup that exists is linked as the dynamic talkgroup of the slot, and
// 0, an empty value or a value that FreeDMR refuses as a default dial
// unlinks the slot, as FreeDMR resets the dial of the slot then. It
// reports whether it applied v.
func (s *Server) dial(ctx context.Context, id uint, slot2 bool, v dmrd.OptionValue) bool {
	if v.State == dmrd.OptionAbsent || s.hub == nil {
		return false
	}
	slot := 1
	if slot2 {
		slot = 2
	}
	if v.State == dmrd.OptionClear || !dialable(v.Value) {
		slog.Info("Unlinking the dynamic talkgroup for the default dial of repeater options", "repeaterID", id, "slot", slot, "dial", v.Value)
		s.hub.UnlinkDynamicTalkgroup(ctx, id, slot2)
		return true
	}
	exists, err := models.TalkgroupIDExists(s.DB, uint(v.Value))
	if err != nil {
		slog.Error("Error finding the default dial talkgroup of repeater options", "repeaterID", id, "error", err)
		return false
	}
	if !exists {
		slog.Info("Ignoring a default dial in repeater options that does not exist", "repeaterID", id, "slot", slot, "talkgroupID", v.Value)
		return false
	}
	slog.Info("Linking the default dial of repeater options", "repeaterID", id, "slot", slot, "talkgroupID", v.Value)
	s.hub.LinkDynamicTalkgroup(ctx, id, uint(v.Value), slot2)
	return true
}

// dialable reports whether FreeDMR takes tg as a default dial: above 0, at
// most 999999, and not 1 to 9 or 9990 to 9999.
// (FreeDMR 546165eb bridge_master.py valid_dial_a_tg_reflector)
func dialable(tg uint32) bool {
	return tg > 9 && tg <= 999999 && (tg < 9990 || tg > 9999)
}

// staticTalkgroups returns the talkgroups that text sets as the static
// talkgroups of slot, nil when it leaves them as they are, and an empty
// list when it clears them. set and bad are what staticOption reports for
// slot. A slot that the text does not name is cleared. A list with an entry
// that is not a talkgroup ID leaves the slot. Talkgroups that DMRHub does
// not have are left out, and a list of only such talkgroups leaves the
// slot.
func (s *Server) staticTalkgroups(id uint, text []byte, slot enums.Slot, set, bad bool) []models.Talkgroup {
	if !set {
		return []models.Talkgroup{}
	}
	if bad {
		slog.Info("Ignoring malformed static talkgroups in repeater options", "repeaterID", id, "slot", slot)
		return nil
	}
	var ids []uint
	for _, tg := range dmrd.AppendFreeDMRStatic(nil, text, slot) {
		if !slices.Contains(ids, uint(tg)) {
			ids = append(ids, uint(tg))
		}
	}
	tgs, err := models.FindTalkgroupsByIDs(s.DB, ids)
	if err != nil {
		slog.Error("Error finding the talkgroups of repeater options", "repeaterID", id, "error", err)
		return nil
	}
	if len(tgs) < len(ids) {
		found := talkgroupIDs(tgs)
		missing := slices.DeleteFunc(ids, func(tg uint) bool { return slices.Contains(found, tg) })
		slog.Info("Ignoring static talkgroups in repeater options that do not exist", "repeaterID", id, "slot", slot, "talkgroups", missing)
		if len(tgs) == 0 {
			return nil
		}
	}
	return tgs
}

func talkgroupIDs(tgs []models.Talkgroup) []uint {
	ids := make([]uint, 0, len(tgs))
	for _, tg := range tgs {
		ids = append(ids, tg.ID)
	}
	return ids
}

// staticOption reports whether text has a static talkgroup key for slot,
// with the keys that dmrd.AppendFreeDMRStatic reads, and whether one of
// its entries is not a talkgroup ID, which AppendFreeDMRStatic would skip.
func staticOption(text []byte, slot enums.Slot) (set, bad bool) {
	text = bytes.TrimRight(text, "\x00")
	short, long, numbered := "TS1", "TS1_STATIC", "TS1_"
	if slot == enums.Slot2 {
		short, long, numbered = "TS2", "TS2_STATIC", "TS2_"
	}
	var values [][]byte
	if v, ok := optionValue(text, numbered+"1"); ok {
		values = append(values, v)
		for d := '2'; d <= '9'; d++ {
			if v, ok := optionValue(text, numbered+string(d)); ok {
				values = append(values, v)
			}
		}
	} else if v, ok := optionValue(text, short); ok {
		values = append(values, v)
	} else if v, ok := optionValue(text, long); ok {
		values = append(values, v)
	}
	for _, v := range values {
		for _, entry := range bytes.Split(v, []byte{','}) {
			if !talkgroupEntry(entry) {
				bad = true
			}
		}
	}
	return len(values) > 0, bad
}

// optionValue returns the value of the last pair of key in text, as
// FreeDMR reads the pairs: those with exactly one "=", with quote
// characters and octets that are not ASCII dropped from the key.
func optionValue(text []byte, key string) ([]byte, bool) {
	var value []byte
	found := false
	for _, pair := range bytes.Split(text, []byte{';'}) {
		if bytes.Count(pair, []byte{'='}) != 1 {
			continue
		}
		eq := bytes.IndexByte(pair, '=')
		k := make([]byte, 0, eq)
		for _, c := range pair[:eq] {
			if c != '\'' && c != '"' && c < 0x80 {
				k = append(k, c)
			}
		}
		if string(k) == key {
			value, found = pair[eq+1:], true
		}
	}
	return value, found
}

// talkgroupEntry reports whether entry of a static talkgroup list is empty
// or a number below 2^24, with white space, quote characters and octets
// that are not ASCII ignored.
func talkgroupEntry(entry []byte) bool {
	var n uint32
	for _, c := range entry {
		switch {
		case c == '\'' || c == '"' || c >= 0x80 || c == ' ' || c >= '\t' && c <= '\r' || c >= 0x1C && c <= 0x1F:
		case c >= '0' && c <= '9':
			n = n*10 + uint32(c-'0')
			if n >= 1<<24 {
				return false
			}
		default:
			return false
		}
	}
	return true
}

// logIgnoredOptions logs the options in text that DMRHub has no feature
// for.
func logIgnoredOptions(id uint, text []byte, o *dmrd.FreeDMROptions) {
	var names []string
	for _, opt := range []struct {
		name string
		v    dmrd.OptionValue
	}{
		{"Timer", o.Timer},
		{"TS1Timer", o.TS1Timer}, {"TS2Timer", o.TS2Timer}, {"TGTimer", o.TGTimer},
		{"DialTimer", o.DialTimer}, {"TS1TGTimer", o.TS1TGTimer}, {"TS2TGTimer", o.TS2TGTimer},
		{"TS1DialTimer", o.TS1DialTimer}, {"TS2DialTimer", o.TS2DialTimer}, {"Voice", o.Voice},
		{"IdentTG", o.IdentTG}, {"Single", o.Single}, {"DialATG", o.DialATG},
		{"Dynamic", o.Dynamic}, {"DirectDialSlot", o.DirectDialSlot},
	} {
		if opt.v.State != dmrd.OptionAbsent {
			names = append(names, opt.name)
		}
	}
	if len(o.Key) > 0 {
		names = append(names, "Key")
	}
	if len(o.Lang) > 0 {
		names = append(names, "Lang")
	}
	if _, ok := optionValue(bytes.TrimRight(text, "\x00"), "UserLink"); ok {
		names = append(names, "UserLink")
	}
	if len(names) > 0 {
		slog.Info("Ignoring repeater options that DMRHub has no feature for", "repeaterID", id, "options", names)
	}
}
