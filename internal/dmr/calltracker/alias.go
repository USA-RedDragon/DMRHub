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

package calltracker

import (
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/dmrgo/v2/layer2/pdu"
	"github.com/USA-RedDragon/dmrgo/v2/layer3"
	"github.com/USA-RedDragon/dmrgo/v2/transport/dmrd"
)

const (
	subPartial      = "partial"
	shortAliasSize  = 15
	shortGPSSize    = 14
	aliasStateIdle  = 10 * time.Minute
	sourceStateHold = voiceHang
)

type sourceKey struct {
	server   string
	repeater uint
	src      uint
}

type sourceState struct {
	asm    pdu.TalkerAliasAssembler
	format string
	dst    uint
	group  bool
	slot   bool
	at     time.Time
}

// TalkerAlias records the talker alias block a that the repeater with the
// ID repeater sent to the server named server. A complete alias makes an
// alias row and names the voice call of its source; a block of the short
// form that DMRGateway sends makes a partial alias row.
func (c *CallTracker) TalkerAlias(a dmrd.TalkerAlias, server string, repeater uint) {
	key := sourceKey{server: server, repeater: repeater, src: uint(a.Source)}
	c.enqueue(c.sourceShard(key), task{at: time.Now(), fn: func(s *shard) { s.alias(key, a) }})
}

// Position records the GPS position p that the repeater with the ID
// repeater sent to the server named server, as a gps row. The short form
// that DMRGateway sends makes a partial row without coordinates.
func (c *CallTracker) Position(p dmrd.Position, server string, repeater uint) {
	key := sourceKey{server: server, repeater: repeater, src: uint(p.Source)}
	c.enqueue(c.sourceShard(key), task{at: time.Now(), fn: func(s *shard) { s.position(key, p) }})
}

func (c *CallTracker) sourceShard(key sourceKey) int {
	if ref, ok := c.sources.Load(key); ok && time.Since(ref.at) < sourceStateHold {
		return ref.shard
	}
	return chanKey{server: key.server, repeater: key.repeater}.shard()
}

func (s *shard) source(key sourceKey) *sourceState {
	st, ok := s.aliases[key]
	if !ok {
		st = &sourceState{at: time.Now()}
		s.aliases[key] = st
	}
	return st
}

func (s *shard) heard(t *task) {
	if t.peer {
		return
	}
	key := sourceKey{server: t.key.server, repeater: t.p.Repeater, src: t.p.Src}
	st := s.source(key)
	st.dst, st.group, st.slot, st.at = t.p.Dst, t.p.GroupCall, t.p.Slot, t.at
}

func (s *shard) pruneSources(now time.Time) {
	for k, st := range s.aliases {
		if now.Sub(st.at) >= aliasStateIdle {
			delete(s.aliases, k)
		}
	}
}

func (s *shard) sourceTask(key sourceKey, st *sourceState) *task {
	return &task{
		key: chanKey{server: key.server, repeater: key.repeater, slot: st.slot},
		p: models.Packet{
			Src:       key.src,
			Dst:       st.dst,
			Repeater:  key.repeater,
			Slot:      st.slot,
			GroupCall: st.group,
		},
		at: time.Now(),
	}
}

func (s *shard) alias(key sourceKey, a dmrd.TalkerAlias) {
	st := s.source(key)
	t := s.sourceTask(key, st)
	ch := s.channel(t.key, false)
	if a.Size == shortAliasSize || a.Size == 0 {
		s.single(t, ch, &class{kind: models.CallKindAlias, sub: subPartial})
		s.mark(key, models.CallKindAlias, func(c *models.Call) { c.Partial = true })
		return
	}
	lc, err := a.LC()
	if err != nil {
		slog.Debug("CallTracker: talker alias block does not decode", "error", err)
		return
	}
	if h, ok := lc.AsTalkerAliasHeader(); ok {
		st.format = aliasFormat(h.TalkerAliasDataFormat)
	}
	text, ok, err := st.asm.Add(&lc)
	if err != nil {
		slog.Debug("CallTracker: talker alias does not decode", "error", err)
		return
	}
	if !ok {
		return
	}
	s.single(t, ch, &class{kind: models.CallKindAlias})
	s.mark(key, models.CallKindAlias, func(c *models.Call) {
		c.TalkerAlias, c.AliasFormat = text, st.format
	})
	for _, r := range s.rows {
		if r.call.Kind == models.CallKindVoice && r.call.SourceID == key.src && r.call.IngressID == key.repeater && r.call.Transport == key.server {
			r.call.TalkerAlias, r.call.AliasFormat = text, st.format
		}
	}
}

func (s *shard) position(key sourceKey, p dmrd.Position) {
	st := s.source(key)
	t := s.sourceTask(key, st)
	ch := s.channel(t.key, false)
	if p.Size == shortGPSSize || p.Size == 0 {
		s.single(t, ch, &class{kind: models.CallKindGPS, sub: subPartial})
		s.mark(key, models.CallKindGPS, func(c *models.Call) { c.Partial = true })
		return
	}
	lc, err := p.LC()
	if err != nil {
		return
	}
	gps, ok := lc.AsGPSInfo()
	if !ok {
		return
	}
	s.single(t, ch, &class{kind: models.CallKindGPS})
	s.mark(key, models.CallKindGPS, func(c *models.Call) {
		lat, lon, pe := gps.Latitude, gps.Longitude, uint8(gps.PositionError)
		c.Latitude, c.Longitude, c.PositionError = &lat, &lon, &pe
	})
}

func (s *shard) mark(key sourceKey, kind models.CallKind, set func(*models.Call)) {
	for k, r := range s.rows {
		sk, ok := k.(singleKey)
		if ok && sk.kind == kind && sk.src == key.src && sk.ch.server == key.server && sk.ch.repeater == key.repeater {
			set(r.call)
		}
	}
}

func aliasFormat(f layer3.TalkerAliasDataFormat) string {
	switch f {
	case layer3.TalkerAliasSevenBit:
		return "7-bit"
	case layer3.TalkerAliasISOEightBit:
		return "8-bit"
	case layer3.TalkerAliasUTF8:
		return "utf-8"
	case layer3.TalkerAliasUTF16:
		return "utf-16"
	}
	return ""
}
