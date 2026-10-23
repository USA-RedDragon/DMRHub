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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
)

const (
	repeaterChangedTopic = "hub:repeaters:changed"
	repeaterCacheTTL     = 10 * time.Second
)

type cachedRepeater struct {
	rpt *models.Repeater
	at  time.Time
}

type repeaterCache struct {
	entries *xsync.Map[uint, cachedRepeater]
	mu      sync.Mutex
	gen     uint64
}

func newRepeaterCache() *repeaterCache {
	return &repeaterCache{entries: xsync.NewMap[uint, cachedRepeater]()}
}

func (c *repeaterCache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *repeaterCache) store(id uint, rpt *models.Repeater, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == gen {
		c.entries.Store(id, cachedRepeater{rpt: rpt, at: time.Now()})
	}
}

func (c *repeaterCache) invalidate(id uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.entries.Delete(id)
}

// routingRepeater returns the repeater as the delivery path sees it. The
// result is shared and must not be modified.
func (h *Hub) routingRepeater(id uint) (*models.Repeater, error) {
	if e, ok := h.repeaters.entries.Load(id); ok && time.Since(e.at) < repeaterCacheTTL {
		return e.rpt, nil
	}
	gen := h.repeaters.generation()
	rpt, err := models.FindRepeaterByID(h.db, id)
	if err != nil {
		return nil, fmt.Errorf("finding repeater %d: %w", id, err)
	}
	h.repeaters.store(id, &rpt, gen)
	return &rpt, nil
}

// repeaterChanged drops the cached repeater here and tells the other
// replicas to drop theirs and reload its subscriptions.
func (h *Hub) repeaterChanged(id uint) {
	h.repeaters.invalidate(id)
	msg := h.origin + ":" + strconv.FormatUint(uint64(id), 10)
	if err := h.pubsub.Publish(repeaterChangedTopic, []byte(msg)); err != nil {
		slog.Error("Failed to announce a repeater change", "repeaterID", id, "error", err)
	}
}

func (h *Hub) listenRepeaterChanges(sub pubsub.Subscription) {
	defer func() {
		if err := sub.Close(); err != nil {
			slog.Error("Error closing repeater change subscription", "error", err)
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
			origin, idStr, found := strings.Cut(string(msg), ":")
			if !found || origin == h.origin {
				continue
			}
			id, err := strconv.ParseUint(idStr, 10, 32)
			if err != nil {
				slog.Error("Invalid repeater change notice", "message", string(msg))
				continue
			}
			h.repeaters.invalidate(uint(id))
			h.reloadLocal(context.Background(), uint(id))
		}
	}
}

// resyncRepeaters reloads, every repeaterCacheTTL, the subscriptions of
// each repeater connected here whose talkgroups differ from the database,
// so a lost change notice heals.
func (h *Hub) resyncRepeaters() {
	ticker := time.NewTicker(repeaterCacheTTL)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			var ids []uint
			h.subscriptionMgr.subscriptions.Range(func(id uint, _ *xsync.Map[uint, *subscription]) bool {
				ids = append(ids, id)
				return true
			})
			for _, id := range ids {
				h.resyncRepeater(id)
			}
		}
	}
}

func (h *Hub) resyncRepeater(id uint) {
	rpt, err := models.FindRepeaterByID(h.db, id)
	if err != nil {
		slog.Error("Failed to find repeater to resync", "repeaterID", id, "error", err)
		return
	}
	want := map[uint]bool{id: true}
	for _, tg := range rpt.TS1StaticTalkgroups {
		want[tg.ID] = true
	}
	for _, tg := range rpt.TS2StaticTalkgroups {
		want[tg.ID] = true
	}
	if rpt.TS1DynamicTalkgroupID != nil {
		want[*rpt.TS1DynamicTalkgroupID] = true
	}
	if rpt.TS2DynamicTalkgroupID != nil {
		want[*rpt.TS2DynamicTalkgroupID] = true
	}
	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(id)
	if !ok {
		return
	}
	have := 0
	same := true
	radioSubs.Range(func(key uint, _ *subscription) bool {
		have++
		same = want[key]
		return same
	})
	if same && have == len(want) {
		return
	}
	slog.Info("Repeater subscriptions differ from the database, reloading", "repeaterID", id)
	h.repeaters.invalidate(id)
	h.reloadLocal(context.Background(), id)
}

func newOrigin() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
