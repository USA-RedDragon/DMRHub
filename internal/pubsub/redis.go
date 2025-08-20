package pubsub

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makePubSubFromRedis(config *config.Config) (redisPubSub, error) {
	return redisPubSub{}, nil
}

type redisPubSub struct {
}
