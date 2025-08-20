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

func (s *GORMStore) Limit(key string, c *gin.Context) (ret ratelimit.Info) {
	ret.Limit = s.limit

	exists, err := models.RatelimitKeyExists(s.db, key)
	if err != nil {
		slog.Error("Failed to check ratelimit key existence", "error", err)
		exists = false
	}
	rl := &models.Ratelimit{
		Key: key,
	}
	if !exists {
		rl.Hits = 0
		rl.Timestamp = time.Now()
	} else {
		rl, err = models.FindRatelimitByKey(s.db, key)
		if err != nil {
			slog.Error("Failed to find ratelimit by key", "error", err)
		}
	}

	ret.ResetTime = time.Now().Add(s.rate - time.Since(rl.Timestamp))

	if rl.Timestamp.Add(s.rate).Before(time.Now()) {
		rl.Hits = 0
	}

	if rl.Hits >= int64(s.limit) {
		ret.RateLimited = true
		ret.RemainingHits = 0

	} else {
		rl.Timestamp = time.Now()
		rl.Hits++
		ret.RemainingHits = s.limit - uint(rl.Hits)
	}

	err = s.db.Save(rl).Error
	if err != nil {
		slog.Error("Failed to save ratelimit entry", "error", err)
	}

	return
}
