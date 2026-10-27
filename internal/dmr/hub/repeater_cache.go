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
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/dmr/lookup"
	"github.com/USA-RedDragon/DMRHub/internal/pubsub"
	"github.com/puzpuzpuz/xsync/v4"
	"gorm.io/gorm"
)

const (
	repeaterChangedTopic = "hub:repeaters:changed"
	repeaterCacheTTL     = 10 * time.Second
)

type cachedRepeater struct {
	rpt        *models.Repeater
	at         time.Time
	refreshing *atomic.Bool
}

type repeaterCache struct {
	entries *xsync.Map[uint, cachedRepeater]
	flights *lookup.Flights[uint, *models.Repeater]
	mu      sync.Mutex
	gen     uint64
}

func newRepeaterCache() *repeaterCache {
	return &repeaterCache{entries: xsync.NewMap[uint, cachedRepeater](), flights: lookup.NewFlights[uint, *models.Repeater]()}
}

func (c *repeaterCache) generation() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.gen
}

func (c *repeaterCache) bump() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	return c.gen
}

func (c *repeaterCache) store(id uint, rpt *models.Repeater, gen uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.gen == gen {
		c.entries.Store(id, cachedRepeater{rpt: rpt, at: time.Now(), refreshing: &atomic.Bool{}})
	}
}

func (c *repeaterCache) drop(id uint) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.gen++
	c.entries.Delete(id)
}

func (c *repeaterCache) prune(now time.Time) {
	c.entries.Range(func(id uint, e cachedRepeater) bool {
		if now.Sub(e.at) > lookup.Idle {
			c.entries.Delete(id)
		}
		return true
	})
}

// routingRepeater returns the repeater as the delivery path sees it. The
// result is shared and must not be modified. A repeater that is not in the
// cache waits for the database at most lookup.Wait.
func (h *Hub) routingRepeater(id uint) (*models.Repeater, error) {
	return h.cachedRepeater(id, false)
}

// waitRepeater is routingRepeater that waits for the database, for the
// work that no burst waits on.
func (h *Hub) waitRepeater(id uint) (*models.Repeater, error) {
	return h.cachedRepeater(id, true)
}

func (h *Hub) cachedRepeater(id uint, wait bool) (*models.Repeater, error) {
	if e, ok := h.repeaters.entries.Load(id); ok {
		if time.Since(e.at) >= repeaterCacheTTL && e.refreshing.CompareAndSwap(false, true) {
			go h.refreshRepeater(id, e)
		}
		if e.rpt == nil {
			return nil, fmt.Errorf("finding repeater %d: %w", id, gorm.ErrRecordNotFound)
		}
		return e.rpt, nil
	}
	gen := h.repeaters.generation()
	load := func() (*models.Repeater, error) { return h.loadRepeater(id, gen) }
	do := h.repeaters.flights.Do
	if wait {
		do = h.repeaters.flights.DoWait
	}
	rpt, err := do(id, load)
	if err != nil {
		return nil, fmt.Errorf("looking up repeater %d: %w", id, err)
	}
	return rpt, nil
}

func (h *Hub) loadRepeater(id uint, gen uint64) (*models.Repeater, error) {
	rpt, err := models.FindRepeaterByID(h.db, id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		h.repeaters.store(id, nil, gen)
	}
	if err != nil {
		return nil, fmt.Errorf("finding repeater %d: %w", id, err)
	}
	h.repeaters.store(id, &rpt, gen)
	return &rpt, nil
}

func (h *Hub) refreshRepeater(id uint, old cachedRepeater) {
	_, err := h.loadRepeater(id, h.repeaters.generation())
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("Cannot reload a repeater, keeping the old one", "repeaterID", id, "error", err)
		h.repeaters.entries.Compute(id, func(cur cachedRepeater, loaded bool) (cachedRepeater, xsync.ComputeOp) {
			if loaded && cur.refreshing == old.refreshing {
				return cachedRepeater{rpt: cur.rpt, at: time.Now(), refreshing: &atomic.Bool{}}, xsync.UpdateOp
			}
			return cur, xsync.CancelOp
		})
	}
	old.refreshing.Store(false)
}

func (h *Hub) reloadRepeater(id uint) {
	if _, err := h.loadRepeater(id, h.repeaters.bump()); err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		slog.Error("Cannot reload a repeater", "repeaterID", id, "error", err)
		h.repeaters.drop(id)
	}
}

// repeaterChanged reloads the cached repeater here and tells the other
// replicas to reload theirs and its subscriptions.
func (h *Hub) repeaterChanged(id uint) {
	h.reloadRepeater(id)
	h.watchers.call(repeaterChangedTopic, id)
	if h.pubsub == nil {
		return
	}
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
			origin, id, valid := parseRepeaterChange(msg)
			if !valid || origin == h.origin {
				continue
			}
			h.reloadRepeater(id)
			h.watchers.call(repeaterChangedTopic, id)
			h.reloadLocal(context.Background(), id)
		}
	}
}

func parseRepeaterChange(msg []byte) (string, uint, bool) {
	origin, idStr, found := strings.Cut(string(msg), ":")
	if !found {
		slog.Error("Invalid repeater change notice", "message", string(msg))
		return "", 0, false
	}
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		slog.Error("Invalid repeater change notice", "message", string(msg))
		return "", 0, false
	}
	return origin, uint(id), true
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
				_ = h.resyncRepeater(id)
			}
		}
	}
}

// resyncRepeater reloads the subscriptions of the repeater id when they
// differ from the database, and reports whether it did.
func (h *Hub) resyncRepeater(id uint) bool {
	rpt, err := models.FindRepeaterByID(h.db, id)
	if err != nil {
		slog.Error("Failed to find repeater to resync", "repeaterID", id, "error", err)
		return false
	}
	want := wantedSubscriptions(&rpt)
	radioSubs, ok := h.subscriptionMgr.subscriptions.Load(id)
	if !ok {
		return false
	}
	have := 0
	same := true
	radioSubs.Range(func(key uint, _ *subscription) bool {
		have++
		same = want[key]
		return same
	})
	if same && have == len(want) {
		return false
	}
	slog.Info("Repeater subscriptions differ from the database, reloading", "repeaterID", id)
	h.reloadRepeater(id)
	h.reloadLocal(context.Background(), id)
	return true
}

func newOrigin() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
