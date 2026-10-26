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

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/metrics"
	"github.com/USA-RedDragon/DMRHub/internal/monotime"
)

// UndeliverableFunc is told of each burst that a target server could not
// carry. packet.Repeater is the target repeater.
type UndeliverableFunc func(packet models.Packet, target, reason string)

// SetUndeliverableFunc sets the function that ReportUndeliverable calls.
func (h *Hub) SetUndeliverableFunc(f UndeliverableFunc) {
	h.undeliverable.Store(&f)
}

const undeliverableLogEvery = time.Second

// ReportUndeliverable counts and logs a burst that target could not carry,
// and tells the UndeliverableFunc. It logs once per stream and at most
// once a second, with the number of streams it did not log since.
func (h *Hub) ReportUndeliverable(packet models.Packet, target, reason string) {
	metrics.NewMetrics().IncrementUndeliverable(target, reason)
	if _, logged := h.undeliverableLogged.LoadOrStore(packet.StreamID, time.Now()); !logged {
		now := monotime.Now()
		next := h.undeliverableLogNext.Load()
		if now >= next && h.undeliverableLogNext.CompareAndSwap(next, now+int64(undeliverableLogEvery)) {
			slog.Info("Burst cannot go to its target",
				"target", target, "repeaterID", packet.Repeater, "stream", packet.StreamID,
				"src", packet.Src, "dst", packet.Dst, "reason", reason,
				"unloggedStreams", h.undeliverableUnlogged.Swap(0))
		} else {
			h.undeliverableUnlogged.Add(1)
		}
	}
	if f := h.undeliverable.Load(); f != nil {
		(*f)(packet, target, reason)
	}
}
