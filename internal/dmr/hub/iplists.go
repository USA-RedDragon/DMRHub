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
	"reflect"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/ipfilter"
)

const ipListsChangedTopic = "hub:iplists:changed"

type ipLists struct {
	src       config.IPLists
	repeaters *ipfilter.List
	peers     *ipfilter.List
}

// SetIPLists makes l the IP lists that the servers check the source
// addresses of their packets with. When l differs from the lists before,
// it calls the funcs of OnIPListsChange.
func (h *Hub) SetIPLists(l config.IPLists) {
	h.ipListsMu.Lock()
	if cur := h.ipLists.Load(); cur != nil && reflect.DeepEqual(cur.src, l) {
		h.ipListsMu.Unlock()
		return
	}
	h.ipLists.Store(&ipLists{src: l, repeaters: ipfilter.Compile(l.Repeaters), peers: ipfilter.Compile(l.Peers)})
	h.ipListsMu.Unlock()
	h.watchers.call(ipListsChangedTopic, 0)
}

// RepeaterIPs returns the IP list of the MMDVM, WebTransport and IPSC
// servers. Without SetIPLists it accepts every address.
func (h *Hub) RepeaterIPs() *ipfilter.List {
	if l := h.ipLists.Load(); l != nil {
		return l.repeaters
	}
	return nil
}

// PeerIPs returns the IP list of the OpenBridge server. Without
// SetIPLists it accepts every address.
func (h *Hub) PeerIPs() *ipfilter.List {
	if l := h.ipLists.Load(); l != nil {
		return l.peers
	}
	return nil
}

// OnIPListsChange calls fn after SetIPLists changes the IP lists, so that a
// server ends the sessions of the addresses that the lists now refuse. The
// returned func stops the calls.
func (h *Hub) OnIPListsChange(fn func()) (stop func()) {
	return h.watchers.add(ipListsChangedTopic, func(uint) { fn() })
}
