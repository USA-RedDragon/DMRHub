package pubsub

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeInMemoryPubSub(config *config.Config) (PubSub, error) {
	return inMemoryPubSub{}, nil
}

type inMemoryPubSub struct {
}
