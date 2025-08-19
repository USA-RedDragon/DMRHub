package kv

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeKVFromRedis(config *config.Config) (KV, error) {
	return redisKV{}, nil
}

type redisKV struct {
}
