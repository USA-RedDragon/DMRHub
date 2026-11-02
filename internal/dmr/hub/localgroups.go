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
	"strconv"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/undeliverable"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
)

const (
	localGroupTopicPrefix = "hub:packets:localgroup:"
	// localKeyBase marks the subscription keys of local groups, above
	// every repeater ID and talkgroup.
	localKeyBase = uint(1 << 31)
)

func localGroupTopic(group uint) string {
	return localGroupTopicPrefix + strconv.FormatUint(uint64(group), 10)
}

func localGroupKey(group uint) uint {
	return localKeyBase | group
}

// localVerdict is what the hub does with a group call to a local
// talkgroup number.
type localVerdict int

const (
	// notLocal is a call that routes as any other.
	notLocal localVerdict = iota
	// localGroupCall is a call that goes to the local group.
	localGroupCall
	// localDrop is a call to a local number that goes nowhere.
	localDrop
)

// localRoute returns what the hub does with packet from sourceName: route
// it to the local group it returns, drop it, or route it as any other. A
// group call to a local number goes to the local group of the repeater on
// that number when it came on the slot of that group. From a peer or a
// plugin, or from a repeater in no such group, it goes nowhere.
func (h *Hub) localRoute(packet models.Packet, sourceName string) (localVerdict, uint) {
	if !packet.GroupCall || !h.routingConfig().IsLocalTalkgroup(packet.Dst) {
		return notLocal, 0
	}
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return localDrop, 0
	}
	rpt, err := h.routingRepeater(packet.Repeater)
	if err != nil {
		countLookupWait(err)
		return localDrop, 0
	}
	m, ok := rpt.LocalGroupOn(packet.Dst)
	if !ok || m.Slot != packet.Slot {
		return localDrop, 0
	}
	return localGroupCall, m.GroupID
}

// LocalGroupOf reports whether dst is a local talkgroup, and returns the
// local group, and its name, that a group call to it on the slot of the
// repeater goes to, as the hub routes it: none, with ID 0, when the call
// goes nowhere.
func (h *Hub) LocalGroupOf(repeaterID, dst uint, slot bool) (uint, string, bool) {
	if !h.routingConfig().IsLocalTalkgroup(dst) {
		return 0, "", false
	}
	rpt, err := h.routingRepeater(repeaterID)
	if err != nil {
		return 0, "", true
	}
	m, ok := rpt.LocalGroupOn(dst)
	if !ok || m.Slot != slot {
		return 0, "", true
	}
	return m.GroupID, m.Name, true
}

// dropLocal reports a call to a local number that goes nowhere as
// undeliverable, when it came from a repeater.
func (h *Hub) dropLocal(packet models.Packet, sourceName string) {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin || h.getServerRole(sourceName) == RolePeer {
		return
	}
	h.ReportUndeliverable(packet, sourceName, undeliverable.ReasonNotInLocalGroup)
}

// localTarget is a repeater of a local group and the slot it takes the
// calls of the group on.
type localTarget struct {
	rpt  *models.Repeater
	slot bool
}

// localTargets returns the repeaters of the local group but src.
func (h *Hub) localTargets(group, src uint) []localTarget {
	ids, err := h.localMembers.Get(group)
	if err != nil {
		countLookupWait(err)
		return nil
	}
	out := make([]localTarget, 0, len(ids))
	for _, id := range ids {
		if id == src {
			continue
		}
		rpt, err := h.routingRepeater(id)
		if err != nil {
			continue
		}
		if m, ok := rpt.LocalGroupByID(group); ok {
			out = append(out, localTarget{rpt: rpt, slot: m.Slot})
		}
	}
	return out
}
