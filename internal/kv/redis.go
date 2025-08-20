package kv

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeKVFromRedis(config *config.Config) (redisKV, error) {
	return redisKV{}, nil
}

type redisKV struct {
}
