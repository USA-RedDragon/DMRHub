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

package hub

import (
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/utils"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

// monitor fans the voice of talkgroups out to the listeners of this
// replica on one feed, which takes the topics of the talkgroups that a
// listener hears. ops orders the changes of the feed; mu guards the
// listeners, which the reader of the feed takes.
type monitor struct {
	ops  sync.Mutex
	feed pubsub.Feed
	mu   sync.RWMutex
	byTG map[uint]map[*listener]struct{}
}

type listener struct {
	tg uint
	fn func(models.Packet)
}

// Listen calls fn with each voice burst of a group call to the talkgroup
// tg that any replica routes, until stop is called. It gets only the
// original bursts, not the copies that the hub converts to the other
// codec. fn runs on one goroutine for every listener of the replica and
// must not block. A listener sends nothing: the hub does not route, track
// or record anything for it.
func (h *Hub) Listen(tg uint, fn func(models.Packet)) (stop func()) {
	m := &h.monitor
	l := &listener{tg: tg, fn: fn}
	m.ops.Lock()
	defer m.ops.Unlock()
	select {
	case <-h.done:
		return func() {}
	default:
	}
	if m.feed == nil {
		m.feed = h.pubsub.Feed()
		feed := m.feed
		h.goTracked(func() { h.readMonitor(feed) })
	}
	m.mu.RLock()
	first := len(m.byTG[tg]) == 0
	m.mu.RUnlock()
	if first {
		m.feed.Add(talkgroupTopic(tg))
	}
	m.mu.Lock()
	if m.byTG == nil {
		m.byTG = map[uint]map[*listener]struct{}{}
	}
	if m.byTG[tg] == nil {
		m.byTG[tg] = map[*listener]struct{}{}
	}
	m.byTG[tg][l] = struct{}{}
	m.mu.Unlock()
	var once sync.Once
	return func() { once.Do(func() { h.unlisten(l) }) }
}

func (h *Hub) unlisten(l *listener) {
	m := &h.monitor
	m.ops.Lock()
	defer m.ops.Unlock()
	m.mu.Lock()
	set := m.byTG[l.tg]
	delete(set, l)
	last := len(set) == 0
	if last {
		delete(m.byTG, l.tg)
	}
	none := len(m.byTG) == 0
	m.mu.Unlock()
	if m.feed == nil {
		return
	}
	if none {
		_ = m.feed.Close()
		m.feed = nil
		return
	}
	if last {
		m.feed.Remove(talkgroupTopic(l.tg))
	}
}

// readMonitor hands the voice bursts of feed to the listeners of their
// talkgroups until feed closes or the hub stops.
func (h *Hub) readMonitor(feed pubsub.Feed) {
	m := &h.monitor
	for {
		var msg pubsub.Message
		var ok bool
		select {
		case <-h.done:
			_ = feed.Close()
			return
		case msg, ok = <-feed.Channel():
		}
		if !ok {
			return
		}
		packet, ok := unmarshalRawPacket(msg.Payload)
		if !ok || !packet.GroupCall || packet.Copy {
			continue
		}
		if voice, _ := utils.CheckPacketType(packet); !voice {
			continue
		}
		m.mu.RLock()
		for l := range m.byTG[packet.Dst] {
			l.fn(packet)
		}
		m.mu.RUnlock()
	}
}
