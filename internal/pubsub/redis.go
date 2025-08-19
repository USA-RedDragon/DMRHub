package pubsub

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makePubSubFromRedis(config *config.Config) (PubSub, error) {
	return redisPubSub{}, nil
}

type redisPubSub struct {
}
