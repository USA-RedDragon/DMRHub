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
	"log/slog"
	"strconv"
	"sync"

	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
)

const (
	talkgroupChangedTopic = "hub:talkgroups:changed"
	userChangedTopic      = "hub:users:changed"
	peerChangedTopic      = "hub:peers:changed"
)

type watchers struct {
	mu   sync.Mutex
	next int
	fns  map[int]watcher
}

type watcher struct {
	topic string
	fn    func(uint)
}

func (w *watchers) add(topic string, fn func(uint)) func() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.fns == nil {
		w.fns = map[int]watcher{}
	}
	id := w.next
	w.next++
	w.fns[id] = watcher{topic: topic, fn: fn}
	return func() {
		w.mu.Lock()
		defer w.mu.Unlock()
		delete(w.fns, id)
	}
}

func (w *watchers) call(topic string, id uint) {
	w.mu.Lock()
	var fns []func(uint)
	for _, x := range w.fns {
		if x.topic == topic {
			fns = append(fns, x.fn)
		}
	}
	w.mu.Unlock()
	for _, fn := range fns {
		fn(id)
	}
}

// OnPeerChange calls fn with the ID of a peer after a change of the peer or
// of its rules on any replica. The returned func stops the calls.
func (h *Hub) OnPeerChange(fn func(peerID uint)) (stop func()) {
	return h.watchers.add(peerChangedTopic, fn)
}

// OnRepeaterChange calls fn with the ID of a repeater after ReloadRepeater
// on any replica. The returned func stops the calls.
func (h *Hub) OnRepeaterChange(fn func(repeaterID uint)) (stop func()) {
	return h.watchers.add(repeaterChangedTopic, fn)
}

// TalkgroupChanged makes every replica read the talkgroup id again. Call it
// after the talkgroup is made, changed or deleted.
func (h *Hub) TalkgroupChanged(id uint) {
	h.announce(talkgroupChangedTopic, id)
}

// UserChanged makes every replica read the user id again. Call it after the
// user is made, changed or deleted.
func (h *Hub) UserChanged(id uint) {
	h.announce(userChangedTopic, id)
}

// PeerChanged makes every replica read the OpenBridge peer id and its rules
// again. Call it after the peer or one of its rules is made, changed or
// deleted.
func (h *Hub) PeerChanged(id uint) {
	h.announce(peerChangedTopic, id)
}

func (h *Hub) announce(topic string, id uint) {
	h.applyChange(topic, id)
	if h.pubsub == nil {
		return
	}
	msg := h.origin + ":" + strconv.FormatUint(uint64(id), 10)
	if err := h.pubsub.Publish(topic, []byte(msg)); err != nil {
		slog.Error("Failed to announce a change", "topic", topic, "id", id, "error", err)
	}
}

func (h *Hub) applyChange(topic string, id uint) {
	switch topic {
	case talkgroupChangedTopic:
		h.talkgroupIDs.Reload(id)
	case userChangedTopic:
		h.radioOwners.Reload(id)
		h.radioCalls.Drop(id)
	case peerChangedTopic:
		h.egressTable.Reload(struct{}{})
		h.watchers.call(peerChangedTopic, id)
	}
}

func (h *Hub) listenChanges(topic string, sub pubsub.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.Error("Error closing a change subscription", "topic", topic, "error", err)
		}
	}()
	ch := sub.Channel()
	for {
		select {
		case <-h.done:
			return
		case msg, ok := <-ch:
			if !ok {
				return
			}
			origin, id, valid := parseRepeaterChange(msg)
			if !valid || origin == h.origin {
				continue
			}
			h.applyChange(topic, id)
		}
	}
}
