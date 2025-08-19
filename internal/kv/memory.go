package kv

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeInMemoryKV(config *config.Config) (KV, error) {
	return inMemoryKV{}, nil
}

type inMemoryKV struct {
}
