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

func (h *Hub) loadRepeaterRules(id uint) (*repeaterRules, error) {
	rules, err := models.ListRoutingRules(h.db, id)
	if err != nil {
		return nil, err //nolint:wrapcheck // ListRoutingRules wraps its errors
	}
	r := &repeaterRules{}
	for _, rule := range rules {
		switch rule.Kind {
		case models.RoutingRuleSlotUser:
			if rule.Slot == 1 || rule.Slot == 2 {
				addRule(&r.slotUsers[rule.Slot-1], rule.SubjectID)
			}
		case models.RoutingRuleBlockTalkgroup:
			addRule(&r.blocked, rule.SubjectID)
		case models.RoutingRuleAllowTalkgroup:
			addRule(&r.allowed, rule.SubjectID)
		}
	}
	return r, nil
}

func (h *Hub) rulesOf(repeaterID uint) *repeaterRules {
	r, err := h.repeaterRules.Get(repeaterID)
	if err != nil {
		slog.Error("Cannot read the routing rules of a repeater", "repeaterID", repeaterID, "error", err)
		return nil
	}
	return r
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

func (h *Hub) refusal(repeaterID uint, slot bool, packet models.Packet, out bool) string {
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
