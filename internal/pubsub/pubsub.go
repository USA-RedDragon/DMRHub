package pubsub

import "github.com/USA-RedDragon/DMRHub/internal/config"

type PubSub interface {
	Publish(topic string, message []byte) error
	Subscribe(topic string) Subscription
	Close() error
}

type Subscription interface {
	Unsubscribe() error
	Close() error
	Channel() <-chan []byte
}

func MakePubSub(config *config.Config) (PubSub, error) {
	// if config.Redis.Enabled {
	// 	return makePubSubFromRedis(config)
	// }
	return makeInMemoryPubSub(config)
}
