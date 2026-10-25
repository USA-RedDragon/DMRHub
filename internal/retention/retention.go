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

// Package retention deletes the records that are older than the retention
// periods of the config.
package retention

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/USA-RedDragon/DMRHub/internal/kv"
	"gorm.io/gorm"
)

const (
	// Interval is the time between two purges.
	Interval  = time.Hour
	lockKey   = "dmrhub:retention:lock"
	lockTTL   = Interval - 5*time.Minute
	batchSize = 1000
	day       = 24 * time.Hour
)

// Result counts what one purge removed.
type Result struct {
	Ran           bool
	Messages      int
	Transmissions int
	Bursts        int
}

// Purger deletes old records in batches. Only one replica purges in each
// interval: the one that takes the lock in the KV store.
type Purger struct {
	cfg *config.Config
	db  *gorm.DB
	kv  kv.KV

	stopOnce sync.Once
	cancel   context.CancelFunc
	done     chan struct{}
}

// NewPurger returns a Purger that reads the retention periods from cfg at
// each purge, so a change in the Setup page takes effect at the next one.
func NewPurger(cfg *config.Config, db *gorm.DB, kvStore kv.KV) *Purger {
	return &Purger{cfg: cfg, db: db, kv: kvStore}
}

// Start runs a purge now and then once each Interval, until Stop.
func (p *Purger) Start(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.done = make(chan struct{})
	go func() {
		defer close(p.done)
		ticker := time.NewTicker(Interval)
		defer ticker.Stop()
		for {
			if _, err := p.Run(ctx, time.Now()); err != nil {
				slog.Error("Retention: purge failed", "error", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
}

// Stop ends the purges that Start runs and waits for the current one.
func (p *Purger) Stop() {
	p.stopOnce.Do(func() {
		if p.cancel != nil {
			p.cancel()
			<-p.done
		}
	})
}

// Run purges the records older than the retention periods at now, and the
// positions older than the retention of their radio, if this replica takes
// the lock.
func (p *Purger) Run(ctx context.Context, now time.Time) (Result, error) {
	r := p.cfg.DMR.Retention
	var res Result
	ok, err := p.kv.SetNX(ctx, lockKey, "1", lockTTL)
	if err != nil {
		return res, fmt.Errorf("retention: cannot take the lock: %w", err)
	}
	if !ok {
		return res, nil
	}
	res.Ran = true
	db := p.db.WithContext(ctx)
	if err := models.PrunePositions(db, now, time.Duration(p.cfg.DMR.Positions.Retention())*day); err != nil {
		return res, fmt.Errorf("retention: %w", err)
	}
	if r.Messages > 0 {
		if res.Messages, err = purgeMessages(db, now.Add(-time.Duration(r.Messages)*day)); err != nil {
			return res, err
		}
	}
	if r.Bursts > 0 {
		if res.Bursts, err = purgeBursts(db, now.Add(-time.Duration(r.Bursts)*day)); err != nil {
			return res, err
		}
	}
	if r.Transmissions > 0 {
		if res.Transmissions, err = purgeTransmissions(db, now.Add(-time.Duration(r.Transmissions)*day)); err != nil {
			return res, err
		}
	}
	if res.Messages+res.Bursts+res.Transmissions > 0 {
		slog.Info("Retention: purged old records", "messages", res.Messages, "transmissions", res.Transmissions, "bursts", res.Bursts)
	}
	return res, nil
}

func purgeMessages(db *gorm.DB, cut time.Time) (int, error) {
	total := 0
	for {
		var ids []uint
		if err := db.Model(&models.Message{}).Where("received_at < ?", cut.UTC()).Order("id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return total, fmt.Errorf("retention: cannot find old messages: %w", err)
		}
		if len(ids) == 0 {
			return total, nil
		}
		if err := db.Where("id IN ?", ids).Delete(&models.Message{}).Error; err != nil {
			return total, fmt.Errorf("retention: cannot delete old messages: %w", err)
		}
		total += len(ids)
		if len(ids) < batchSize {
			return total, nil
		}
	}
}

func purgeBursts(db *gorm.DB, cut time.Time) (int, error) {
	total := 0
	for {
		var ids []uint
		if err := db.Unscoped().Model(&models.Call{}).Where("start_time < ? AND call_data IS NOT NULL", cut.UTC()).Order("id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return total, fmt.Errorf("retention: cannot find old bursts: %w", err)
		}
		if len(ids) == 0 {
			return total, nil
		}
		if err := db.Unscoped().Model(&models.Call{}).Where("id IN ?", ids).UpdateColumn("call_data", gorm.Expr("NULL")).Error; err != nil {
			return total, fmt.Errorf("retention: cannot delete old bursts: %w", err)
		}
		total += len(ids)
		if len(ids) < batchSize {
			return total, nil
		}
	}
}

func purgeTransmissions(db *gorm.DB, cut time.Time) (int, error) {
	total := 0
	for {
		var ids []uint
		if err := db.Unscoped().Model(&models.Call{}).Where("start_time < ? AND active = ?", cut.UTC(), false).Order("id").Limit(batchSize).Pluck("id", &ids).Error; err != nil {
			return total, fmt.Errorf("retention: cannot find old transmissions: %w", err)
		}
		if len(ids) == 0 {
			return total, nil
		}
		err := db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Model(&models.Message{}).Where("transmission_id IN ?", ids).UpdateColumn("transmission_id", gorm.Expr("NULL")).Error; err != nil {
				return fmt.Errorf("cannot unlink messages: %w", err)
			}
			if err := tx.Unscoped().Where("id IN ?", ids).Delete(&models.Call{}).Error; err != nil {
				return fmt.Errorf("cannot delete old transmissions: %w", err)
			}
			return nil
		})
		if err != nil {
			return total, fmt.Errorf("retention: %w", err)
		}
		total += len(ids)
		if len(ids) < batchSize {
			return total, nil
		}
	}
}
