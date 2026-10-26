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

package ratelimit

import (
	"fmt"
	"log/slog"
	"time"

	ratelimit "github.com/JGLTechnologies/gin-rate-limit"
	"github.com/USA-RedDragon/DMRHub/internal/db/models"
	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type GORMStore struct {
	db    *gorm.DB
	rate  time.Duration
	limit uint
}

type GORMOptions struct {
	DB    *gorm.DB
	Rate  time.Duration
	Limit uint
}

func NewGORMStore(options *GORMOptions) *GORMStore {
	return &GORMStore{
		db:    options.DB,
		rate:  options.Rate,
		limit: options.Limit,
	}
}

// Limit counts a request of key and reports whether it is over the limit
// of its window, which starts with the first request after the last window
// ended and lasts the rate. The count is written and read back under the
// lock of its row, so concurrent requests of one key each see their own
// count.
func (s *GORMStore) Limit(key string, _ *gin.Context) (ret ratelimit.Info) {
	ret.Limit = s.limit
	now := time.Now()
	rl, err := s.hit(key, now)
	if err != nil {
		slog.Error("Failed to count a ratelimit hit", "error", err)
		ret.RemainingHits = s.limit
		ret.ResetTime = now.Add(s.rate)
		return ret
	}
	ret.ResetTime = rl.Timestamp.Add(s.rate)
	if rl.Hits < 0 || uint64(rl.Hits) > uint64(s.limit) {
		ret.RateLimited = true
		return ret
	}
	ret.RemainingHits = s.limit - uint(uint64(rl.Hits))
	return ret
}

func (s *GORMStore) hit(key string, now time.Time) (models.Ratelimit, error) {
	var rl models.Ratelimit
	windowStart := now.Add(-s.rate)
	if s.db.Name() == "mysql" {
		err := s.db.Transaction(func(tx *gorm.DB) error {
			if err := tx.Exec("INSERT INTO ratelimits (`key`, hits, `timestamp`) VALUES (?, 1, ?)\n"+
				"ON DUPLICATE KEY UPDATE\n"+
				"\thits = IF(`timestamp` < ?, 1, hits + 1),\n"+
				"\t`timestamp` = IF(`timestamp` < ?, ?, `timestamp`)",
				key, now, windowStart, windowStart, now).Error; err != nil {
				return fmt.Errorf("count the hit: %w", err)
			}
			if err := tx.Raw("SELECT `key`, hits, `timestamp` FROM ratelimits WHERE `key` = ?", key).Scan(&rl).Error; err != nil {
				return fmt.Errorf("read the count: %w", err)
			}
			return nil
		})
		if err != nil {
			return rl, fmt.Errorf("ratelimit %s: %w", key, err)
		}
		return rl, nil
	}
	err := s.db.Raw(`INSERT INTO ratelimits (key, hits, timestamp) VALUES (?, 1, ?)
ON CONFLICT (key) DO UPDATE SET
	hits = CASE WHEN ratelimits.timestamp < ? THEN 1 ELSE ratelimits.hits + 1 END,
	timestamp = CASE WHEN ratelimits.timestamp < ? THEN excluded.timestamp ELSE ratelimits.timestamp END
RETURNING key, hits, timestamp`, key, now, windowStart, windowStart).Scan(&rl).Error
	if err != nil {
		return rl, fmt.Errorf("ratelimit %s: %w", key, err)
	}
	return rl, nil
}
