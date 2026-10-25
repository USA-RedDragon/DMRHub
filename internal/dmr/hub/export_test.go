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
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
)

// HoldStream keeps the routing worker of stream busy until release is
// called.
func (h *Hub) HoldStream(stream uint) (release func()) {
	ch := make(chan struct{})
	h.routing.Do(context.Background(), stream, func() { <-ch })
	return func() { close(ch) }
}

func (h *Hub) SwitchDynamicTalkgroup(ctx context.Context, packet models.Packet) {
	h.switchDynamicTalkgroup(ctx, packet)
}

// ForgetCachedRepeater drops the cached repeater, so that the next delivery
// to it reads the database.
func (h *Hub) ForgetCachedRepeater(id uint) {
	h.repeaters.invalidate(id)
}

// SetStreamClaimTiming replaces streamClaimHold and streamClaimRecheck.
func (h *Hub) SetStreamClaimTiming(hold, recheck time.Duration) {
	h.claimTiming.Store(&[2]time.Duration{hold, recheck})
}

// StreamClaimHold is streamClaimHold.
const StreamClaimHold = streamClaimHold

// LiftSlotRate lets the test send faster than one repeater slot can, or
// limits it again.
func (h *Hub) LiftSlotRate(lift bool) {
	h.slotRateOff.Store(lift)
}

// HeardAt returns the repeater where the radio userID was last heard, or 0.
func (h *Hub) HeardAt(ctx context.Context, userID uint) uint {
	v, err := h.kv.Get(ctx, fmt.Sprintf("%s%d", lastSeenKeyPrefix, userID))
	if err != nil {
		return 0
	}
	id, err := strconv.ParseUint(string(v), 10, 32)
	if err != nil {
		return 0
	}
	return uint(id)
}
