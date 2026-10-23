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
// ended and lasts the rate. The count is read and written by one statement,
// so concurrent requests of one key each see their own count.
func (s *GORMStore) Limit(key string, _ *gin.Context) (ret ratelimit.Info) {
	ret.Limit = s.limit
	now := time.Now()
	var rl models.Ratelimit
	err := s.db.Raw(`INSERT INTO ratelimits (key, hits, timestamp) VALUES (?, 1, ?)
ON CONFLICT (key) DO UPDATE SET
	hits = CASE WHEN ratelimits.timestamp < ? THEN 1 ELSE ratelimits.hits + 1 END,
	timestamp = CASE WHEN ratelimits.timestamp < ? THEN excluded.timestamp ELSE ratelimits.timestamp END
RETURNING key, hits, timestamp`, key, now, now.Add(-s.rate), now.Add(-s.rate)).Scan(&rl).Error
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
