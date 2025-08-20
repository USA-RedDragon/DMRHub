package kv

import (
	"time"

	"github.com/USA-RedDragon/DMRHub/internal/config"
)

type KV interface {
	Has(key string) (bool, error)
	Get(key string) ([]byte, error)
	Set(key string, value []byte) error
	Delete(key string) error
	Expire(key string, ttl time.Duration) error
	Scan(cursor uint64, match string, count int64) ([]string, uint64, error)
	LLen(key string) (int64, error)
	LIndex(key string, index int64) ([]byte, error)
	RPush(key string, values ...[]byte) (int64, error)
	Close() error
}

// MakeKV creates a new key-value store client.
func MakeKV(config *config.Config) (KV, error) {
	// if config.Redis.Enabled {
	// 	return makeKVFromRedis(config)
	// }
	return makeInMemoryKV(config)
}
