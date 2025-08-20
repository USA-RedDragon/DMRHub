package pubsub

import "github.com/USA-RedDragon/DMRHub/internal/config"

func makeInMemoryPubSub(config *config.Config) (PubSub, error) {
	return inMemoryPubSub{}, nil
}

type inMemoryPubSub struct {
}

func (ps inMemoryPubSub) Publish(topic string, message []byte) error {
	return nil
}

func (ps inMemoryPubSub) Subscribe(topic string) Subscription {
	return inMemorySubscription{
		ch: make(chan []byte),
	}
}

func (ps inMemoryPubSub) Close() error {
	return nil
}

type inMemorySubscription struct {
	ch chan []byte
}

func (s inMemorySubscription) Unsubscribe() error {
	return nil
}

func (s inMemorySubscription) Close() error {
	close(s.ch)
	return nil
}

func (s inMemorySubscription) Channel() <-chan []byte {
	return s.ch
}
