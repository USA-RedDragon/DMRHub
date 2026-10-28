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
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/plugins"
	"github.com/USA-RedDragon/DMRHub/internal/userdb"
)

// streamPolicy is what the routing settings decided about a stream of a
// radio, kept so that the later bursts of the stream need no lookup.
type streamPolicy struct {
	start   time.Time
	last    time.Time
	checked bool
	refused bool
	logged  bool
}

// admitRouting reports whether the routing settings let the hub take
// packet from sourceName in. Plugins are never refused. The answer for a
// stream comes from its first burst, so that the later bursts need no
// lookup.
func (h *Hub) admitRouting(packet models.Packet, sourceName string) bool {
	if _, isPlugin := plugins.PluginOfSource(sourceName); isPlugin {
		return true
	}
	r := h.routingConfig()
	if r.Transmit == "" || r.Transmit == config.TransmitAny {
		return true
	}
	now := time.Now()
	key := claimKey{stream: packet.StreamID, src: packet.Src}
	p, _ := h.streamPolicies.LoadOrCompute(key, func() (streamPolicy, bool) {
		return streamPolicy{start: now}, false
	})
	p.last = now
	if !p.checked {
		allowed, final := h.mayTransmit(r.Transmit, packet, sourceName)
		p.checked, p.refused = final, !allowed
	}
	if p.refused {
		h.refuseStream(key, &p, packet, sourceName, "transmit")
	}
	h.streamPolicies.Store(key, p)
	return !p.refused
}

// mayTransmit reports whether the radio of packet may transmit under
// policy, and whether the answer is final for its stream.
func (h *Hub) mayTransmit(policy config.Transmit, packet models.Packet, sourceName string) (bool, bool) {
	if h.getServerRole(sourceName) == RolePeer {
		return true, true
	}
	switch policy {
	case config.TransmitRadioID:
		_, ok := userdb.Get(packet.Src)
		return ok, true
	case config.TransmitMembers:
		owner, err := h.radioOwners.Get(packet.Src)
		if err != nil {
			slog.Warn("Cannot find the owner of a radio, dropping its burst", "src", packet.Src, "error", err)
			return false, false
		}
		return owner != 0, true
	case config.TransmitAny:
		return true, true
	default:
		return true, true
	}
}

func (h *Hub) refuseStream(key claimKey, p *streamPolicy, packet models.Packet, sourceName, setting string) {
	metrics.NewMetrics().IncrementIngressDropped(metrics.DropReasonTransmit)
	if p.logged {
		return
	}
	p.logged = true
	slog.Info("Dropping a transmission that the routing settings refuse", "setting", setting,
		"stream", key.stream, "src", packet.Src, "dst", packet.Dst, "server", sourceName, "repeaterID", packet.Repeater)
}

func (h *Hub) pruneStreamPolicies(now time.Time) {
	h.streamPolicies.Range(func(k claimKey, p streamPolicy) bool {
		if now.Sub(p.last) > sightedStreamTTL {
			h.streamPolicies.Delete(k)
		}
		return true
	})
}
