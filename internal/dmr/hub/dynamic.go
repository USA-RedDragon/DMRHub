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
	"log/slog"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	dynamicSweepMax = time.Second
	dynamicSweepMin = 50 * time.Millisecond
)

// repeaterSlot is a timeslot of a repeater; slot true is TS2.
type repeaterSlot struct {
	repeater uint
	slot     bool
}

// dynamicUse is the last time that a repeater transmitted on the dynamic
// talkgroup of a timeslot, or that the hub saw the talkgroup linked there.
type dynamicUse struct {
	tg uint
	at time.Time
}

// routingConfig returns the routing settings that the hub runs with.
func (h *Hub) routingConfig() config.Routing {
	if src := h.cfg.Load(); src != nil {
		if c := (*src).Get(); c != nil {
			return c.DMR.Routing
		}
	}
	return config.Routing{}
}

// noteDynamicUse restarts the timeout of the dynamic talkgroup tg of a
// timeslot.
func (h *Hub) noteDynamicUse(repeater uint, slot bool, tg uint) {
	h.dynamicUses.Store(repeaterSlot{repeater: repeater, slot: slot}, dynamicUse{tg: tg, at: time.Now()})
}

func dynamicSweepEvery(timeout time.Duration) time.Duration {
	if timeout <= 0 {
		return dynamicSweepMax
	}
	return min(max(timeout/4, dynamicSweepMin), dynamicSweepMax)
}

// runDynamicTimeouts unlinks, until the hub stops, the dynamic talkgroups
// of the repeaters active on this hub that had no transmission from the
// repeater on them for the dynamic timeout.
func (h *Hub) runDynamicTimeouts() {
	timer := time.NewTimer(dynamicSweepEvery(h.routingConfig().DynamicTimeout))
	defer timer.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-timer.C:
		}
		timeout := h.routingConfig().DynamicTimeout
		if timeout > 0 {
			h.sweepDynamic(timeout)
		}
		timer.Reset(dynamicSweepEvery(h.routingConfig().DynamicTimeout))
	}
}

func (h *Hub) sweepDynamic(timeout time.Duration) {
	now := time.Now()
	h.subscriptionMgr.subscriptions.Range(func(id uint, _ *xsync.Map[uint, *subscription]) bool {
		rpt, err := h.routingRepeater(id)
		if err != nil {
			return true
		}
		for _, s := range []struct {
			slot bool
			tg   *uint
		}{{false, rpt.TS1DynamicTalkgroupID}, {true, rpt.TS2DynamicTalkgroupID}} {
			key := repeaterSlot{repeater: id, slot: s.slot}
			if s.tg == nil {
				h.dynamicUses.Delete(key)
				continue
			}
			use, ok := h.dynamicUses.Load(key)
			if !ok || use.tg != *s.tg {
				h.dynamicUses.Store(key, dynamicUse{tg: *s.tg, at: now})
				continue
			}
			if now.Sub(use.at) < timeout {
				continue
			}
			tg, slot := *s.tg, s.slot
			h.queueLink(context.Background(), id, func() { h.expireDynamic(id, slot, tg, timeout) })
		}
		return true
	})
}

// expireDynamic unlinks tg from the timeslot when it is still its dynamic
// talkgroup and still had no transmission for timeout.
func (h *Hub) expireDynamic(id uint, slot bool, tg uint, timeout time.Duration) {
	key := repeaterSlot{repeater: id, slot: slot}
	if use, ok := h.dynamicUses.Load(key); ok && use.tg == tg && time.Since(use.at) < timeout {
		return
	}
	slog.Info("Unlinking a dynamic talkgroup after its timeout", "repeaterID", id, "slot2", slot, "talkgroupID", tg, "timeout", timeout)
	h.unlinkDynamicIf(id, slot, tg)
	h.dynamicUses.Delete(key)
}
