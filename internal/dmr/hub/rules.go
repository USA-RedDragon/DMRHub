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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
)

const unlinkTalkgroup = 4000

type repeaterRules struct {
	slotUsers [2]map[uint]struct{}
	blocked   map[uint]struct{}
	allowed   map[uint]struct{}
}

func addRule(set *map[uint]struct{}, id uint) {
	if *set == nil {
		*set = map[uint]struct{}{}
	}
	(*set)[id] = struct{}{}
}

type ruleTable struct {
	repeaters map[uint]*repeaterRules
	access    map[uint]*talkgroupAccess
}

func (h *Hub) loadRules() {
	if h.db == nil {
		return
	}
	h.rulesMu.Lock()
	defer h.rulesMu.Unlock()
	rules, err := models.ListAllRoutingRules(h.db)
	if err != nil {
		slog.Error("Cannot read the routing rules, keeping the old ones", "error", err)
		return
	}
	t := &ruleTable{repeaters: map[uint]*repeaterRules{}, access: map[uint]*talkgroupAccess{}}
	repeater := func(id uint) *repeaterRules {
		r, ok := t.repeaters[id]
		if !ok {
			r = &repeaterRules{}
			t.repeaters[id] = r
		}
		return r
	}
	access := func(tg uint) *talkgroupAccess {
		a, ok := t.access[tg]
		if !ok {
			a = &talkgroupAccess{}
			t.access[tg] = a
		}
		return a
	}
	for _, rule := range rules {
		switch rule.Kind {
		case models.RoutingRuleSlotUser:
			if rule.Slot == 1 || rule.Slot == 2 {
				addRule(&repeater(rule.RepeaterID).slotUsers[rule.Slot-1], rule.SubjectID)
			}
		case models.RoutingRuleBlockTalkgroup:
			addRule(&repeater(rule.RepeaterID).blocked, rule.SubjectID)
		case models.RoutingRuleAllowTalkgroup:
			addRule(&repeater(rule.RepeaterID).allowed, rule.SubjectID)
		case models.RoutingRuleRestrictedTalkgroup:
			access(rule.SubjectID).restricted = true
		case models.RoutingRuleApprovedLink:
			addRule(&access(rule.SubjectID).approved, rule.RepeaterID)
		}
	}
	h.rules.Store(t)
}

func (h *Hub) rulesOf(repeaterID uint) *repeaterRules {
	t := h.rules.Load()
	if t == nil {
		return nil
	}
	return t.repeaters[repeaterID]
}

func (h *Hub) slotRefuses(r *repeaterRules, slot bool, packet models.Packet, out bool) bool {
	i := 0
	if slot {
		i = 1
	}
	users := r.slotUsers[i]
	if len(users) == 0 {
		return false
	}
	if h.listed(users, packet.Src) {
		return false
	}
	return !out || packet.GroupCall || !h.listed(users, packet.Dst)
}

func (h *Hub) listed(users map[uint]struct{}, radio uint) bool {
	owner, err := h.radioOwners.Get(radio)
	if err != nil {
		slog.Error("Cannot find the owner of a radio on a locked slot", "radio", radio, "error", err)
		return false
	}
	_, ok := users[owner]
	return ok
}

func (h *Hub) talkgroupRefused(r *repeaterRules, tg uint) bool {
	if r.blocked == nil && r.allowed == nil {
		return false
	}
	if _, ok := r.blocked[tg]; ok {
		return true
	}
	if r.allowed == nil {
		return false
	}
	_, ok := r.allowed[tg]
	return !ok
}

type talkgroupAccess struct {
	restricted bool
	approved   map[uint]struct{}
}

func (h *Hub) mayLink(repeaterID, tg uint) bool {
	t := h.rules.Load()
	if t == nil {
		return true
	}
	a, ok := t.access[tg]
	if !ok || !a.restricted {
		return true
	}
	_, ok = a.approved[repeaterID]
	return ok
}

func (h *Hub) refusal(repeaterID uint, slot bool, packet models.Packet, out bool) string {
	if packet.GroupCall && packet.Dst != unlinkTalkgroup && !h.mayLink(repeaterID, packet.Dst) {
		return undeliverable.ReasonTalkgroupRestricted
	}
	r := h.rulesOf(repeaterID)
	if r == nil {
		return ""
	}
	if h.slotRefuses(r, slot, packet, out) {
		return undeliverable.ReasonSlotLocked
	}
	if packet.GroupCall && packet.Dst != unlinkTalkgroup && h.talkgroupRefused(r, packet.Dst) {
		return undeliverable.ReasonTalkgroupRefused
	}
	return ""
}

func (h *Hub) refuseIngress(packet models.Packet, sourceName string) bool {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return false
	}
	reason := h.refusal(packet.Repeater, packet.Slot, packet, false)
	if reason == "" {
		return false
	}
	h.ReportUndeliverable(packet, sourceName, reason)
	return true
}
